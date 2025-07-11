// Licensed to the LF AI & Data foundation under one
// or more contributor license agreements. See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership. The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License. You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package job

import (
	"context"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/samber/lo"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"

	"github.com/milvus-io/milvus-proto/go-api/v2/milvuspb"
	"github.com/milvus-io/milvus-proto/go-api/v2/rgpb"
	"github.com/milvus-io/milvus-proto/go-api/v2/schemapb"
	etcdkv "github.com/milvus-io/milvus/internal/kv/etcd"
	"github.com/milvus-io/milvus/internal/metastore"
	"github.com/milvus-io/milvus/internal/metastore/kv/querycoord"
	"github.com/milvus-io/milvus/internal/metastore/mocks"
	"github.com/milvus-io/milvus/internal/querycoordv2/checkers"
	"github.com/milvus-io/milvus/internal/querycoordv2/meta"
	"github.com/milvus-io/milvus/internal/querycoordv2/observers"
	. "github.com/milvus-io/milvus/internal/querycoordv2/params"
	"github.com/milvus-io/milvus/internal/querycoordv2/session"
	"github.com/milvus-io/milvus/internal/querycoordv2/utils"
	"github.com/milvus-io/milvus/internal/util/proxyutil"
	"github.com/milvus-io/milvus/pkg/v2/kv"
	"github.com/milvus-io/milvus/pkg/v2/proto/datapb"
	"github.com/milvus-io/milvus/pkg/v2/proto/querypb"
	"github.com/milvus-io/milvus/pkg/v2/util/etcd"
	"github.com/milvus-io/milvus/pkg/v2/util/merr"
	"github.com/milvus-io/milvus/pkg/v2/util/paramtable"
	"github.com/milvus-io/milvus/pkg/v2/util/typeutil"
)

const (
	defaultVecFieldID = 1
	defaultIndexID    = 1
)

type JobSuite struct {
	suite.Suite

	// Data
	collections []int64
	partitions  map[int64][]int64
	channels    map[int64][]string
	segments    map[int64]map[int64][]int64 // CollectionID, PartitionID -> Segments
	loadTypes   map[int64]querypb.LoadType

	// Dependencies
	kv                 kv.MetaKv
	store              metastore.QueryCoordCatalog
	dist               *meta.DistributionManager
	meta               *meta.Meta
	cluster            *session.MockCluster
	targetMgr          *meta.TargetManager
	targetObserver     *observers.TargetObserver
	collectionObserver *observers.CollectionObserver
	broker             *meta.MockBroker
	nodeMgr            *session.NodeManager
	checkerController  *checkers.CheckerController
	proxyManager       *proxyutil.MockProxyClientManager

	// Test objects
	scheduler *Scheduler

	ctx context.Context
}

func (suite *JobSuite) SetupSuite() {
	paramtable.Init()

	suite.collections = []int64{1000, 1001}
	suite.partitions = map[int64][]int64{
		1000: {100, 101, 102},
		1001: {103, 104, 105},
	}
	suite.channels = map[int64][]string{
		1000: {"1000-dmc0", "1000-dmc1"},
		1001: {"1001-dmc0", "1001-dmc1"},
	}
	suite.segments = map[int64]map[int64][]int64{
		1000: {
			100: {1, 2},
			101: {3, 4},
			102: {5, 6},
		},
		1001: {
			103: {7, 8},
			104: {9, 10},
			105: {11, 12},
		},
	}
	suite.loadTypes = map[int64]querypb.LoadType{
		1000: querypb.LoadType_LoadCollection,
		1001: querypb.LoadType_LoadPartition,
	}

	suite.broker = meta.NewMockBroker(suite.T())
	for collection, partitions := range suite.segments {
		vChannels := []*datapb.VchannelInfo{}
		for _, channel := range suite.channels[collection] {
			vChannels = append(vChannels, &datapb.VchannelInfo{
				CollectionID: collection,
				ChannelName:  channel,
			})
		}

		segmentBinlogs := []*datapb.SegmentInfo{}
		for partition, segments := range partitions {
			for _, segment := range segments {
				segmentBinlogs = append(segmentBinlogs, &datapb.SegmentInfo{
					ID:            segment,
					PartitionID:   partition,
					InsertChannel: suite.channels[collection][segment%2],
				})
			}
		}
		suite.broker.EXPECT().GetRecoveryInfoV2(mock.Anything, collection).Return(vChannels, segmentBinlogs, nil).Maybe()
	}

	suite.broker.EXPECT().DescribeCollection(mock.Anything, mock.Anything).
		Return(&milvuspb.DescribeCollectionResponse{
			Schema: &schemapb.CollectionSchema{
				Fields: []*schemapb.FieldSchema{
					{FieldID: 100},
					{FieldID: 101},
					{FieldID: 102},
				},
			},
		}, nil)
	suite.broker.EXPECT().ListIndexes(mock.Anything, mock.Anything).
		Return(nil, nil).Maybe()

	suite.cluster = session.NewMockCluster(suite.T())
	suite.cluster.EXPECT().SyncDistribution(mock.Anything, mock.Anything, mock.Anything).Return(merr.Success(), nil).Maybe()

	suite.proxyManager = proxyutil.NewMockProxyClientManager(suite.T())
	suite.proxyManager.EXPECT().InvalidateCollectionMetaCache(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	suite.proxyManager.EXPECT().InvalidateShardLeaderCache(mock.Anything, mock.Anything).Return(nil).Maybe()
}

func (suite *JobSuite) SetupTest() {
	config := GenerateEtcdConfig()
	cli, err := etcd.GetEtcdClient(
		config.UseEmbedEtcd.GetAsBool(),
		config.EtcdUseSSL.GetAsBool(),
		config.Endpoints.GetAsStrings(),
		config.EtcdTLSCert.GetValue(),
		config.EtcdTLSKey.GetValue(),
		config.EtcdTLSCACert.GetValue(),
		config.EtcdTLSMinVersion.GetValue())
	suite.Require().NoError(err)
	suite.kv = etcdkv.NewEtcdKV(cli, config.MetaRootPath.GetValue())
	suite.ctx = context.Background()

	suite.store = querycoord.NewCatalog(suite.kv)
	suite.dist = meta.NewDistributionManager()
	suite.nodeMgr = session.NewNodeManager()
	suite.meta = meta.NewMeta(RandomIncrementIDAllocator(), suite.store, suite.nodeMgr)
	suite.targetMgr = meta.NewTargetManager(suite.broker, suite.meta)
	suite.targetObserver = observers.NewTargetObserver(suite.meta,
		suite.targetMgr,
		suite.dist,
		suite.broker,
		suite.cluster,
		suite.nodeMgr,
	)
	suite.targetObserver.Start()
	suite.scheduler = NewScheduler()

	suite.scheduler.Start()
	meta.GlobalFailedLoadCache = meta.NewFailedLoadCache()

	suite.nodeMgr.Add(session.NewNodeInfo(session.ImmutableNodeInfo{
		NodeID:   1000,
		Address:  "localhost",
		Hostname: "localhost",
	}))
	suite.nodeMgr.Add(session.NewNodeInfo(session.ImmutableNodeInfo{
		NodeID:   2000,
		Address:  "localhost",
		Hostname: "localhost",
	}))
	suite.nodeMgr.Add(session.NewNodeInfo(session.ImmutableNodeInfo{
		NodeID:   3000,
		Address:  "localhost",
		Hostname: "localhost",
	}))

	suite.meta.HandleNodeUp(suite.ctx, 1000)
	suite.meta.HandleNodeUp(suite.ctx, 2000)
	suite.meta.HandleNodeUp(suite.ctx, 3000)

	suite.checkerController = &checkers.CheckerController{}
	suite.collectionObserver = observers.NewCollectionObserver(
		suite.dist,
		suite.meta,
		suite.targetMgr,
		suite.targetObserver,
		suite.checkerController,
		suite.proxyManager,
	)
}

func (suite *JobSuite) TearDownTest() {
	suite.kv.Close()
	suite.scheduler.Stop()
	suite.targetObserver.Stop()
}

func (suite *JobSuite) BeforeTest(suiteName, testName string) {
	for collection, partitions := range suite.partitions {
		suite.broker.EXPECT().
			GetPartitions(mock.Anything, collection).
			Return(partitions, nil).Maybe()
	}
}

func (suite *JobSuite) TestLoadCollection() {
	ctx := context.Background()

	// Test load collection
	for _, collection := range suite.collections {
		if suite.loadTypes[collection] != querypb.LoadType_LoadCollection {
			continue
		}
		// Load with 1 replica
		req := &querypb.LoadCollectionRequest{
			CollectionID: collection,
			// It will be set to 1
			// ReplicaNumber: 1,
		}
		job := NewLoadCollectionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			false,
		)
		suite.scheduler.Add(job)
		err := job.Wait()
		suite.NoError(err)
		suite.EqualValues(1, suite.meta.GetReplicaNumber(ctx, collection))
		suite.targetMgr.UpdateCollectionCurrentTarget(ctx, collection)
		suite.assertCollectionLoaded(collection)
	}

	// Test load again
	for _, collection := range suite.collections {
		if suite.loadTypes[collection] != querypb.LoadType_LoadCollection {
			continue
		}
		req := &querypb.LoadCollectionRequest{
			CollectionID: collection,
		}
		job := NewLoadCollectionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			false,
		)
		suite.scheduler.Add(job)
		err := job.Wait()
		suite.NoError(err)
	}

	// Test load existed collection with different replica number
	for _, collection := range suite.collections {
		if suite.loadTypes[collection] != querypb.LoadType_LoadCollection {
			continue
		}
		req := &querypb.LoadCollectionRequest{
			CollectionID:  collection,
			ReplicaNumber: 3,
		}
		job := NewLoadCollectionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			false,
		)
		suite.scheduler.Add(job)
		err := job.Wait()
		suite.ErrorIs(err, merr.ErrParameterInvalid)
	}

	// Test load partition while collection exists
	for _, collection := range suite.collections {
		if suite.loadTypes[collection] != querypb.LoadType_LoadCollection {
			continue
		}
		// Load with 1 replica
		req := &querypb.LoadPartitionsRequest{
			CollectionID:  collection,
			PartitionIDs:  suite.partitions[collection],
			ReplicaNumber: 1,
		}
		job := NewLoadPartitionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			false,
		)
		suite.scheduler.Add(job)
		err := job.Wait()
		suite.NoError(err)
	}

	cfg := &rgpb.ResourceGroupConfig{
		Requests: &rgpb.ResourceGroupLimit{
			NodeNum: 0,
		},
		Limits: &rgpb.ResourceGroupLimit{
			NodeNum: 0,
		},
	}

	suite.meta.ResourceManager.AddResourceGroup(ctx, "rg1", cfg)
	suite.meta.ResourceManager.AddResourceGroup(ctx, "rg2", cfg)
	suite.meta.ResourceManager.AddResourceGroup(ctx, "rg3", cfg)

	// Load with 3 replica on 1 rg
	req := &querypb.LoadCollectionRequest{
		CollectionID:   1001,
		ReplicaNumber:  3,
		ResourceGroups: []string{"rg1"},
	}
	job := NewLoadCollectionJob(
		ctx,
		req,
		suite.dist,
		suite.meta,
		suite.broker,
		suite.targetMgr,
		suite.targetObserver,
		suite.collectionObserver,
		suite.nodeMgr,
		false,
	)
	suite.scheduler.Add(job)
	err := job.Wait()
	suite.ErrorIs(err, merr.ErrResourceGroupNodeNotEnough)

	// Load with 3 replica on 3 rg
	req = &querypb.LoadCollectionRequest{
		CollectionID:   1001,
		ReplicaNumber:  3,
		ResourceGroups: []string{"rg1", "rg2", "rg3"},
	}
	job = NewLoadCollectionJob(
		ctx,
		req,
		suite.dist,
		suite.meta,
		suite.broker,
		suite.targetMgr,
		suite.targetObserver,
		suite.collectionObserver,
		suite.nodeMgr,
		false,
	)
	suite.scheduler.Add(job)
	err = job.Wait()
	suite.ErrorIs(err, merr.ErrResourceGroupNodeNotEnough)
}

func (suite *JobSuite) TestLoadCollectionWithReplicas() {
	ctx := context.Background()

	// Test load collection
	for _, collection := range suite.collections {
		if suite.loadTypes[collection] != querypb.LoadType_LoadCollection {
			continue
		}
		// Load with 3 replica
		req := &querypb.LoadCollectionRequest{
			CollectionID:  collection,
			ReplicaNumber: 5,
		}
		job := NewLoadCollectionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			false,
		)
		suite.scheduler.Add(job)
		err := job.Wait()
		suite.ErrorIs(err, merr.ErrResourceGroupNodeNotEnough)
	}
}

func (suite *JobSuite) TestLoadCollectionWithLoadFields() {
	ctx := context.Background()

	suite.Run("init_load", func() {
		// Test load collection
		for _, collection := range suite.collections {
			if suite.loadTypes[collection] != querypb.LoadType_LoadCollection {
				continue
			}
			// Load with 1 replica
			req := &querypb.LoadCollectionRequest{
				CollectionID: collection,
				LoadFields:   []int64{100, 101, 102},
			}
			job := NewLoadCollectionJob(
				ctx,
				req,
				suite.dist,
				suite.meta,
				suite.broker,
				suite.targetMgr,
				suite.targetObserver,
				suite.collectionObserver,
				suite.nodeMgr,
				false,
			)
			suite.scheduler.Add(job)
			err := job.Wait()
			suite.NoError(err)
			suite.EqualValues(1, suite.meta.GetReplicaNumber(ctx, collection))
			suite.targetMgr.UpdateCollectionCurrentTarget(ctx, collection)
			suite.assertCollectionLoaded(collection)
		}
	})

	suite.Run("load_again_same_fields", func() {
		for _, collection := range suite.collections {
			if suite.loadTypes[collection] != querypb.LoadType_LoadCollection {
				continue
			}
			req := &querypb.LoadCollectionRequest{
				CollectionID: collection,
				LoadFields:   []int64{102, 101, 100}, // field id order shall not matter
			}
			job := NewLoadCollectionJob(
				ctx,
				req,
				suite.dist,
				suite.meta,
				suite.broker,
				suite.targetMgr,
				suite.targetObserver,
				suite.collectionObserver,
				suite.nodeMgr,
				false,
			)
			suite.scheduler.Add(job)
			err := job.Wait()
			suite.NoError(err)
		}
	})

	suite.Run("load_again_diff_fields", func() {
		// Test load existed collection with different load fields
		for _, collection := range suite.collections {
			if suite.loadTypes[collection] != querypb.LoadType_LoadCollection {
				continue
			}
			req := &querypb.LoadCollectionRequest{
				CollectionID: collection,
				LoadFields:   []int64{100, 101},
			}
			job := NewLoadCollectionJob(
				ctx,
				req,
				suite.dist,
				suite.meta,
				suite.broker,
				suite.targetMgr,
				suite.targetObserver,
				suite.collectionObserver,
				suite.nodeMgr,
				false,
			)
			suite.scheduler.Add(job)
			err := job.Wait()
			// suite.ErrorIs(err, merr.ErrParameterInvalid)
			suite.NoError(err)
		}
	})

	suite.Run("load_from_legacy_proxy", func() {
		// Test load again with legacy proxy
		for _, collection := range suite.collections {
			if suite.loadTypes[collection] != querypb.LoadType_LoadCollection {
				continue
			}
			req := &querypb.LoadCollectionRequest{
				CollectionID: collection,
				Schema: &schemapb.CollectionSchema{
					Fields: []*schemapb.FieldSchema{
						{FieldID: 100},
						{FieldID: 101},
						{FieldID: 102},
					},
				},
			}
			job := NewLoadCollectionJob(
				ctx,
				req,
				suite.dist,
				suite.meta,
				suite.broker,
				suite.targetMgr,
				suite.targetObserver,
				suite.collectionObserver,
				suite.nodeMgr,
				false,
			)
			suite.scheduler.Add(job)
			err := job.Wait()
			suite.NoError(err)
		}
	})
}

func (suite *JobSuite) TestLoadPartition() {
	ctx := context.Background()

	// Test load partition
	for _, collection := range suite.collections {
		if suite.loadTypes[collection] != querypb.LoadType_LoadPartition {
			continue
		}
		// Load with 1 replica
		req := &querypb.LoadPartitionsRequest{
			CollectionID:  collection,
			PartitionIDs:  suite.partitions[collection],
			ReplicaNumber: 1,
		}
		job := NewLoadPartitionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			false,
		)
		suite.scheduler.Add(job)
		err := job.Wait()
		suite.NoError(err)
		suite.EqualValues(1, suite.meta.GetReplicaNumber(ctx, collection))
		suite.targetMgr.UpdateCollectionCurrentTarget(ctx, collection)
		suite.assertCollectionLoaded(collection)
	}

	// Test load partition again
	for _, collection := range suite.collections {
		if suite.loadTypes[collection] != querypb.LoadType_LoadPartition {
			continue
		}
		// Load with 1 replica
		req := &querypb.LoadPartitionsRequest{
			CollectionID: collection,
			PartitionIDs: suite.partitions[collection],
			// ReplicaNumber: 1,
		}
		job := NewLoadPartitionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			false,
		)
		suite.scheduler.Add(job)
		err := job.Wait()
		suite.NoError(err)
	}

	// Test load partition with different replica number
	for _, collection := range suite.collections {
		if suite.loadTypes[collection] != querypb.LoadType_LoadPartition {
			continue
		}

		req := &querypb.LoadPartitionsRequest{
			CollectionID:  collection,
			PartitionIDs:  suite.partitions[collection],
			ReplicaNumber: 3,
		}
		job := NewLoadPartitionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			false,
		)
		suite.scheduler.Add(job)
		err := job.Wait()
		suite.ErrorIs(err, merr.ErrParameterInvalid)
	}

	// Test load partition with more partition
	for _, collection := range suite.collections {
		if suite.loadTypes[collection] != querypb.LoadType_LoadPartition {
			continue
		}

		req := &querypb.LoadPartitionsRequest{
			CollectionID:  collection,
			PartitionIDs:  append(suite.partitions[collection], 200),
			ReplicaNumber: 1,
		}
		job := NewLoadPartitionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			false,
		)
		suite.scheduler.Add(job)
		err := job.Wait()
		suite.NoError(err)
	}

	// Test load collection while partitions exists
	for _, collection := range suite.collections {
		if suite.loadTypes[collection] != querypb.LoadType_LoadPartition {
			continue
		}

		req := &querypb.LoadCollectionRequest{
			CollectionID:  collection,
			ReplicaNumber: 1,
		}
		job := NewLoadCollectionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			false,
		)
		suite.scheduler.Add(job)
		err := job.Wait()
		suite.NoError(err)
	}

	cfg := &rgpb.ResourceGroupConfig{
		Requests: &rgpb.ResourceGroupLimit{
			NodeNum: 1,
		},
		Limits: &rgpb.ResourceGroupLimit{
			NodeNum: 1,
		},
	}
	suite.meta.ResourceManager.AddResourceGroup(ctx, "rg1", cfg)
	suite.meta.ResourceManager.AddResourceGroup(ctx, "rg2", cfg)
	suite.meta.ResourceManager.AddResourceGroup(ctx, "rg3", cfg)

	// test load 3 replica in 1 rg, should pass rg check
	req := &querypb.LoadPartitionsRequest{
		CollectionID:   999,
		PartitionIDs:   []int64{888},
		ReplicaNumber:  3,
		ResourceGroups: []string{"rg1"},
	}
	job := NewLoadPartitionJob(
		ctx,
		req,
		suite.dist,
		suite.meta,
		suite.broker,
		suite.targetMgr,
		suite.targetObserver,
		suite.collectionObserver,
		suite.nodeMgr,
		false,
	)
	suite.scheduler.Add(job)
	err := job.Wait()
	suite.ErrorIs(err, merr.ErrResourceGroupNodeNotEnough)

	// test load 3 replica in 3 rg, should pass rg check
	req = &querypb.LoadPartitionsRequest{
		CollectionID:   999,
		PartitionIDs:   []int64{888},
		ReplicaNumber:  3,
		ResourceGroups: []string{"rg1", "rg2", "rg3"},
	}
	job = NewLoadPartitionJob(
		ctx,
		req,
		suite.dist,
		suite.meta,
		suite.broker,
		suite.targetMgr,
		suite.targetObserver,
		suite.collectionObserver,
		suite.nodeMgr,
		false,
	)
	suite.scheduler.Add(job)
	err = job.Wait()
	suite.ErrorIs(err, merr.ErrResourceGroupNodeNotEnough)
}

func (suite *JobSuite) TestLoadPartitionWithLoadFields() {
	ctx := context.Background()

	suite.Run("init_load", func() {
		// Test load partition
		for _, collection := range suite.collections {
			if suite.loadTypes[collection] != querypb.LoadType_LoadPartition {
				continue
			}
			// Load with 1 replica
			req := &querypb.LoadPartitionsRequest{
				CollectionID:  collection,
				PartitionIDs:  suite.partitions[collection],
				ReplicaNumber: 1,
				LoadFields:    []int64{100, 101, 102},
			}
			job := NewLoadPartitionJob(
				ctx,
				req,
				suite.dist,
				suite.meta,
				suite.broker,
				suite.targetMgr,
				suite.targetObserver,
				suite.collectionObserver,
				suite.nodeMgr,
				false,
			)
			suite.scheduler.Add(job)
			err := job.Wait()
			suite.NoError(err)
			suite.EqualValues(1, suite.meta.GetReplicaNumber(ctx, collection))
			suite.targetMgr.UpdateCollectionCurrentTarget(ctx, collection)
			suite.assertCollectionLoaded(collection)
		}
	})

	suite.Run("load_with_same_load_fields", func() {
		for _, collection := range suite.collections {
			if suite.loadTypes[collection] != querypb.LoadType_LoadPartition {
				continue
			}
			// Load with 1 replica
			req := &querypb.LoadPartitionsRequest{
				CollectionID:  collection,
				PartitionIDs:  suite.partitions[collection],
				ReplicaNumber: 1,
				LoadFields:    []int64{102, 101, 100},
			}
			job := NewLoadPartitionJob(
				ctx,
				req,
				suite.dist,
				suite.meta,
				suite.broker,
				suite.targetMgr,
				suite.targetObserver,
				suite.collectionObserver,
				suite.nodeMgr,
				false,
			)
			suite.scheduler.Add(job)
			err := job.Wait()
			suite.NoError(err)
		}
	})

	suite.Run("load_with_diff_load_fields", func() {
		// Test load partition with different load fields
		for _, collection := range suite.collections {
			if suite.loadTypes[collection] != querypb.LoadType_LoadPartition {
				continue
			}

			req := &querypb.LoadPartitionsRequest{
				CollectionID: collection,
				PartitionIDs: suite.partitions[collection],
				LoadFields:   []int64{100, 101},
			}
			job := NewLoadPartitionJob(
				ctx,
				req,
				suite.dist,
				suite.meta,
				suite.broker,
				suite.targetMgr,
				suite.targetObserver,
				suite.collectionObserver,
				suite.nodeMgr,
				false,
			)
			suite.scheduler.Add(job)
			err := job.Wait()
			suite.NoError(err)
		}
	})

	suite.Run("load_legacy_proxy", func() {
		for _, collection := range suite.collections {
			if suite.loadTypes[collection] != querypb.LoadType_LoadPartition {
				continue
			}
			// Load with 1 replica
			req := &querypb.LoadPartitionsRequest{
				CollectionID:  collection,
				PartitionIDs:  suite.partitions[collection],
				ReplicaNumber: 1,
				Schema: &schemapb.CollectionSchema{
					Fields: []*schemapb.FieldSchema{
						{FieldID: 100},
						{FieldID: 101},
						{FieldID: 102},
					},
				},
			}
			job := NewLoadPartitionJob(
				ctx,
				req,
				suite.dist,
				suite.meta,
				suite.broker,
				suite.targetMgr,
				suite.targetObserver,
				suite.collectionObserver,
				suite.nodeMgr,
				false,
			)
			suite.scheduler.Add(job)
			err := job.Wait()
			suite.NoError(err)
		}
	})
}

func (suite *JobSuite) TestDynamicLoad() {
	ctx := context.Background()

	collection := suite.collections[0]
	p0, p1, p2 := suite.partitions[collection][0], suite.partitions[collection][1], suite.partitions[collection][2]
	newLoadPartJob := func(partitions ...int64) *LoadPartitionJob {
		req := &querypb.LoadPartitionsRequest{
			CollectionID:  collection,
			PartitionIDs:  partitions,
			ReplicaNumber: 1,
		}
		job := NewLoadPartitionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			false,
		)
		return job
	}
	newLoadColJob := func() *LoadCollectionJob {
		req := &querypb.LoadCollectionRequest{
			CollectionID:  collection,
			ReplicaNumber: 1,
		}
		job := NewLoadCollectionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			false,
		)
		return job
	}

	// loaded: none
	// action: load p0, p1, p2
	// expect: p0, p1, p2 loaded
	job := newLoadPartJob(p0, p1, p2)
	suite.scheduler.Add(job)
	err := job.Wait()
	suite.NoError(err)
	suite.targetMgr.UpdateCollectionCurrentTarget(ctx, collection)
	suite.assertPartitionLoaded(collection, p0, p1, p2)

	// loaded: p0, p1, p2
	// action: load p0, p1, p2
	// expect: do nothing, p0, p1, p2 loaded
	job = newLoadPartJob(p0, p1, p2)
	suite.scheduler.Add(job)
	err = job.Wait()
	suite.NoError(err)
	suite.assertPartitionLoaded(collection)

	// loaded: p0, p1
	// action: load p2
	// expect: p0, p1, p2 loaded
	suite.releaseAll()
	job = newLoadPartJob(p0, p1)
	suite.scheduler.Add(job)
	err = job.Wait()
	suite.NoError(err)
	suite.targetMgr.UpdateCollectionCurrentTarget(ctx, collection)
	suite.assertPartitionLoaded(collection, p0, p1)
	job = newLoadPartJob(p2)
	suite.scheduler.Add(job)
	err = job.Wait()
	suite.NoError(err)
	suite.targetMgr.UpdateCollectionCurrentTarget(ctx, collection)
	suite.assertPartitionLoaded(collection, p2)

	// loaded: p0, p1
	// action: load p1, p2
	// expect: p0, p1, p2 loaded
	suite.releaseAll()
	job = newLoadPartJob(p0, p1)
	suite.scheduler.Add(job)
	err = job.Wait()
	suite.NoError(err)
	suite.targetMgr.UpdateCollectionCurrentTarget(ctx, collection)
	suite.assertPartitionLoaded(collection, p0, p1)
	job = newLoadPartJob(p1, p2)
	suite.scheduler.Add(job)
	err = job.Wait()
	suite.NoError(err)
	suite.targetMgr.UpdateCollectionCurrentTarget(ctx, collection)
	suite.assertPartitionLoaded(collection, p2)

	// loaded: p0, p1
	// action: load col
	// expect: col loaded
	suite.releaseAll()
	job = newLoadPartJob(p0, p1)
	suite.scheduler.Add(job)
	err = job.Wait()
	suite.NoError(err)
	suite.targetMgr.UpdateCollectionCurrentTarget(ctx, collection)
	suite.assertPartitionLoaded(collection, p0, p1)
	colJob := newLoadColJob()
	suite.scheduler.Add(colJob)
	err = colJob.Wait()
	suite.NoError(err)
	suite.targetMgr.UpdateCollectionCurrentTarget(ctx, collection)
	suite.assertPartitionLoaded(collection, p2)
}

func (suite *JobSuite) TestLoadPartitionWithReplicas() {
	ctx := context.Background()

	// Test load partitions
	for _, collection := range suite.collections {
		if suite.loadTypes[collection] != querypb.LoadType_LoadPartition {
			continue
		}
		// Load with 3 replica
		req := &querypb.LoadPartitionsRequest{
			CollectionID:  collection,
			PartitionIDs:  suite.partitions[collection],
			ReplicaNumber: 5,
		}
		job := NewLoadPartitionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			false,
		)
		suite.scheduler.Add(job)
		err := job.Wait()
		suite.ErrorIs(err, merr.ErrResourceGroupNodeNotEnough)
	}
}

func (suite *JobSuite) TestReleaseCollection() {
	ctx := context.Background()

	suite.loadAll()

	// Test release collection and partition
	for _, collection := range suite.collections {
		req := &querypb.ReleaseCollectionRequest{
			CollectionID: collection,
		}
		job := NewReleaseCollectionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,

			suite.checkerController,
			suite.proxyManager,
		)
		suite.scheduler.Add(job)
		err := job.Wait()
		suite.NoError(err)
		suite.assertCollectionReleased(collection)
	}

	// Test release again
	for _, collection := range suite.collections {
		req := &querypb.ReleaseCollectionRequest{
			CollectionID: collection,
		}
		job := NewReleaseCollectionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.checkerController,
			suite.proxyManager,
		)
		suite.scheduler.Add(job)
		err := job.Wait()
		suite.NoError(err)
		suite.assertCollectionReleased(collection)
	}
}

func (suite *JobSuite) TestReleasePartition() {
	ctx := context.Background()

	suite.loadAll()

	// Test release partition
	for _, collection := range suite.collections {
		req := &querypb.ReleasePartitionsRequest{
			CollectionID: collection,
			PartitionIDs: suite.partitions[collection],
		}
		job := NewReleasePartitionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.checkerController,
			suite.proxyManager,
		)
		suite.scheduler.Add(job)
		err := job.Wait()
		suite.NoError(err)
		suite.assertPartitionReleased(collection, suite.partitions[collection]...)
	}

	// Test release again
	for _, collection := range suite.collections {
		req := &querypb.ReleasePartitionsRequest{
			CollectionID: collection,
			PartitionIDs: suite.partitions[collection],
		}
		job := NewReleasePartitionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.checkerController,
			suite.proxyManager,
		)
		suite.scheduler.Add(job)
		err := job.Wait()
		suite.NoError(err)
		suite.assertPartitionReleased(collection, suite.partitions[collection]...)
	}

	// Test release partial partitions
	suite.releaseAll()
	suite.loadAll()
	for _, collectionID := range suite.collections {
		// make collection able to get into loaded state
		suite.updateChannelDist(ctx, collectionID, true)
		suite.updateSegmentDist(collectionID, 3000, suite.partitions[collectionID]...)
		waitCurrentTargetUpdated(ctx, suite.targetObserver, collectionID)
	}
	for _, collection := range suite.collections {
		req := &querypb.ReleasePartitionsRequest{
			CollectionID: collection,
			PartitionIDs: suite.partitions[collection][1:],
		}
		job := NewReleasePartitionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.checkerController,
			suite.proxyManager,
		)
		suite.scheduler.Add(job)
		suite.updateChannelDist(ctx, collection, true)
		suite.updateSegmentDist(collection, 3000, suite.partitions[collection][:1]...)
		err := job.Wait()
		suite.NoError(err)
		suite.True(suite.meta.Exist(ctx, collection))
		partitions := suite.meta.GetPartitionsByCollection(ctx, collection)
		suite.Len(partitions, 1)
		suite.Equal(suite.partitions[collection][0], partitions[0].GetPartitionID())
		suite.assertPartitionReleased(collection, suite.partitions[collection][1:]...)
	}
}

func (suite *JobSuite) TestDynamicRelease() {
	ctx := context.Background()

	col0, col1 := suite.collections[0], suite.collections[1]
	p0, p1, p2 := suite.partitions[col0][0], suite.partitions[col0][1], suite.partitions[col0][2]
	p3, p4, p5 := suite.partitions[col1][0], suite.partitions[col1][1], suite.partitions[col1][2]
	newReleasePartJob := func(col int64, partitions ...int64) *ReleasePartitionJob {
		req := &querypb.ReleasePartitionsRequest{
			CollectionID: col,
			PartitionIDs: partitions,
		}
		job := NewReleasePartitionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.checkerController,
			suite.proxyManager,
		)
		return job
	}
	newReleaseColJob := func(col int64) *ReleaseCollectionJob {
		req := &querypb.ReleaseCollectionRequest{
			CollectionID: col,
		}
		job := NewReleaseCollectionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.checkerController,
			suite.proxyManager,
		)
		return job
	}

	// loaded: p0, p1, p2
	// action: release p0
	// expect: p0 released, p1, p2 loaded
	suite.loadAll()
	for _, collectionID := range suite.collections {
		// make collection able to get into loaded state
		suite.updateChannelDist(ctx, collectionID, true)
		suite.updateSegmentDist(collectionID, 3000, suite.partitions[collectionID]...)
		waitCurrentTargetUpdated(ctx, suite.targetObserver, collectionID)
	}

	job := newReleasePartJob(col0, p0)
	suite.scheduler.Add(job)
	// update segments
	suite.updateSegmentDist(col0, 3000, p1, p2)
	suite.updateChannelDist(ctx, col0, true)
	err := job.Wait()
	suite.NoError(err)
	suite.assertPartitionReleased(col0, p0)
	suite.assertPartitionLoaded(col0, p1, p2)

	// loaded: p1, p2
	// action: release p0, p1
	// expect: p1 released, p2 loaded
	job = newReleasePartJob(col0, p0, p1)
	suite.scheduler.Add(job)
	suite.updateSegmentDist(col0, 3000, p2)
	suite.updateChannelDist(ctx, col0, true)
	err = job.Wait()
	suite.NoError(err)
	suite.assertPartitionReleased(col0, p0, p1)
	suite.assertPartitionLoaded(col0, p2)

	// loaded: p2
	// action: release p2
	// expect: loadType=col: col loaded, p2 released
	job = newReleasePartJob(col0, p2)
	suite.scheduler.Add(job)
	suite.updateSegmentDist(col0, 3000)
	suite.updateChannelDist(ctx, col0, false)
	err = job.Wait()
	suite.NoError(err)
	suite.assertPartitionReleased(col0, p0, p1, p2)
	suite.False(suite.meta.Exist(ctx, col0))

	// loaded: p0, p1, p2
	// action: release col
	// expect: col released
	suite.releaseAll()
	suite.loadAll()
	releaseColJob := newReleaseColJob(col0)
	suite.scheduler.Add(releaseColJob)
	err = releaseColJob.Wait()
	suite.NoError(err)
	suite.assertCollectionReleased(col0)
	suite.assertPartitionReleased(col0, p0, p1, p2)

	// loaded: p3, p4, p5
	// action: release p3, p4, p5
	// expect: loadType=partition: col released
	suite.releaseAll()
	suite.loadAll()
	job = newReleasePartJob(col1, p3, p4, p5)
	suite.scheduler.Add(job)
	err = job.Wait()
	suite.NoError(err)
	suite.assertCollectionReleased(col1)
	suite.assertPartitionReleased(col1, p3, p4, p5)
}

func (suite *JobSuite) TestLoadCollectionStoreFailed() {
	ctx := context.Background()
	// Store collection failed
	store := mocks.NewQueryCoordCatalog(suite.T())
	suite.meta = meta.NewMeta(RandomIncrementIDAllocator(), store, suite.nodeMgr)

	store.EXPECT().SaveResourceGroup(mock.Anything, mock.Anything).Return(nil)
	suite.meta.HandleNodeUp(ctx, 1000)
	suite.meta.HandleNodeUp(ctx, 2000)
	suite.meta.HandleNodeUp(ctx, 3000)

	for _, collection := range suite.collections {
		if suite.loadTypes[collection] != querypb.LoadType_LoadCollection {
			continue
		}
		suite.broker.EXPECT().GetPartitions(mock.Anything, collection).Return(suite.partitions[collection], nil)
		err := errors.New("failed to store collection")
		store.EXPECT().SaveReplica(mock.Anything, mock.Anything).Return(nil)
		store.EXPECT().SaveCollection(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(err)
		store.EXPECT().ReleaseReplicas(mock.Anything, collection).Return(nil)

		req := &querypb.LoadCollectionRequest{
			CollectionID: collection,
		}
		job := NewLoadCollectionJob(
			context.Background(),
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			false,
		)
		suite.scheduler.Add(job)
		loadErr := job.Wait()
		suite.ErrorIs(loadErr, err)
	}
}

func (suite *JobSuite) TestLoadPartitionStoreFailed() {
	ctx := context.Background()
	// Store partition failed
	store := mocks.NewQueryCoordCatalog(suite.T())
	suite.meta = meta.NewMeta(RandomIncrementIDAllocator(), store, suite.nodeMgr)

	store.EXPECT().SaveResourceGroup(mock.Anything, mock.Anything).Return(nil)
	suite.meta.HandleNodeUp(ctx, 1000)
	suite.meta.HandleNodeUp(ctx, 2000)
	suite.meta.HandleNodeUp(ctx, 3000)

	err := errors.New("failed to store collection")
	for _, collection := range suite.collections {
		if suite.loadTypes[collection] != querypb.LoadType_LoadPartition {
			continue
		}

		store.EXPECT().SaveReplica(mock.Anything, mock.Anything).Return(nil)
		store.EXPECT().SaveCollection(mock.Anything, mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(err)
		store.EXPECT().ReleaseReplicas(mock.Anything, collection).Return(nil)

		req := &querypb.LoadPartitionsRequest{
			CollectionID: collection,
			PartitionIDs: suite.partitions[collection],
		}
		job := NewLoadPartitionJob(
			context.Background(),
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			false,
		)
		suite.scheduler.Add(job)
		loadErr := job.Wait()
		suite.ErrorIs(loadErr, err)
	}
}

func (suite *JobSuite) TestLoadCreateReplicaFailed() {
	// Store replica failed
	suite.meta = meta.NewMeta(ErrorIDAllocator(), suite.store, session.NewNodeManager())
	for _, collection := range suite.collections {
		suite.broker.EXPECT().
			GetPartitions(mock.Anything, collection).
			Return(suite.partitions[collection], nil)
		req := &querypb.LoadCollectionRequest{
			CollectionID: collection,
		}
		job := NewLoadCollectionJob(
			context.Background(),
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			false,
		)
		suite.scheduler.Add(job)
		err := job.Wait()
		suite.ErrorIs(err, merr.ErrResourceGroupNodeNotEnough)
	}
}

func (suite *JobSuite) TestSyncNewCreatedPartition() {
	newPartition := int64(999)
	ctx := context.Background()

	// test sync new created partition
	suite.loadAll()
	collectionID := suite.collections[0]
	// make collection able to get into loaded state
	suite.updateChannelDist(ctx, collectionID, true)
	suite.updateSegmentDist(collectionID, 3000, suite.partitions[collectionID]...)

	req := &querypb.SyncNewCreatedPartitionRequest{
		CollectionID: collectionID,
		PartitionID:  newPartition,
	}
	job := NewSyncNewCreatedPartitionJob(
		ctx,
		req,
		suite.meta,
		suite.broker,
		suite.targetObserver,
		suite.targetMgr,
	)
	suite.scheduler.Add(job)
	err := job.Wait()
	suite.NoError(err)
	partition := suite.meta.CollectionManager.GetPartition(ctx, newPartition)
	suite.NotNil(partition)
	suite.Equal(querypb.LoadStatus_Loaded, partition.GetStatus())

	// test collection not loaded
	req = &querypb.SyncNewCreatedPartitionRequest{
		CollectionID: int64(888),
		PartitionID:  newPartition,
	}
	job = NewSyncNewCreatedPartitionJob(
		ctx,
		req,
		suite.meta,
		suite.broker,
		suite.targetObserver,
		suite.targetMgr,
	)
	suite.scheduler.Add(job)
	err = job.Wait()
	suite.NoError(err)

	// test collection loaded, but its loadType is loadPartition
	req = &querypb.SyncNewCreatedPartitionRequest{
		CollectionID: suite.collections[1],
		PartitionID:  newPartition,
	}
	job = NewSyncNewCreatedPartitionJob(
		ctx,
		req,
		suite.meta,
		suite.broker,
		suite.targetObserver,
		suite.targetMgr,
	)
	suite.scheduler.Add(job)
	err = job.Wait()
	suite.NoError(err)
}

func (suite *JobSuite) loadAll() {
	ctx := context.Background()
	for _, collection := range suite.collections {
		if suite.loadTypes[collection] == querypb.LoadType_LoadCollection {
			req := &querypb.LoadCollectionRequest{
				CollectionID: collection,
			}
			job := NewLoadCollectionJob(
				ctx,
				req,
				suite.dist,
				suite.meta,
				suite.broker,
				suite.targetMgr,
				suite.targetObserver,
				suite.collectionObserver,
				suite.nodeMgr,
				false,
			)
			suite.scheduler.Add(job)
			err := job.Wait()
			suite.NoError(err)
			suite.EqualValues(1, suite.meta.GetReplicaNumber(ctx, collection))
			suite.True(suite.meta.Exist(ctx, collection))
			suite.NotNil(suite.meta.GetCollection(ctx, collection))
			suite.NotNil(suite.meta.GetPartitionsByCollection(ctx, collection))
			suite.targetMgr.UpdateCollectionCurrentTarget(ctx, collection)
		} else {
			req := &querypb.LoadPartitionsRequest{
				CollectionID: collection,
				PartitionIDs: suite.partitions[collection],
			}
			job := NewLoadPartitionJob(
				ctx,
				req,
				suite.dist,
				suite.meta,
				suite.broker,
				suite.targetMgr,
				suite.targetObserver,
				suite.collectionObserver,
				suite.nodeMgr,
				false,
			)
			suite.scheduler.Add(job)
			err := job.Wait()
			suite.NoError(err)
			suite.EqualValues(1, suite.meta.GetReplicaNumber(ctx, collection))
			suite.True(suite.meta.Exist(ctx, collection))
			suite.NotNil(suite.meta.GetCollection(ctx, collection))
			suite.NotNil(suite.meta.GetPartitionsByCollection(ctx, collection))
			suite.targetMgr.UpdateCollectionCurrentTarget(ctx, collection)
		}
	}
}

func (suite *JobSuite) releaseAll() {
	ctx := context.Background()
	for _, collection := range suite.collections {
		req := &querypb.ReleaseCollectionRequest{
			CollectionID: collection,
		}
		job := NewReleaseCollectionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.checkerController,
			suite.proxyManager,
		)
		suite.scheduler.Add(job)
		err := job.Wait()
		suite.NoError(err)
		suite.assertCollectionReleased(collection)
	}
}

func (suite *JobSuite) assertCollectionLoaded(collection int64) {
	ctx := context.Background()
	suite.True(suite.meta.Exist(ctx, collection))
	suite.NotEqual(0, len(suite.meta.ReplicaManager.GetByCollection(ctx, collection)))
	for _, channel := range suite.channels[collection] {
		suite.NotNil(suite.targetMgr.GetDmChannel(ctx, collection, channel, meta.CurrentTarget))
	}
	for _, segments := range suite.segments[collection] {
		for _, segment := range segments {
			suite.NotNil(suite.targetMgr.GetSealedSegment(ctx, collection, segment, meta.CurrentTarget))
		}
	}
}

func (suite *JobSuite) assertPartitionLoaded(collection int64, partitionIDs ...int64) {
	ctx := context.Background()
	suite.True(suite.meta.Exist(ctx, collection))
	suite.NotEqual(0, len(suite.meta.ReplicaManager.GetByCollection(ctx, collection)))
	for _, channel := range suite.channels[collection] {
		suite.NotNil(suite.targetMgr.GetDmChannel(ctx, collection, channel, meta.CurrentTarget))
	}
	for partitionID, segments := range suite.segments[collection] {
		if !lo.Contains(partitionIDs, partitionID) {
			continue
		}
		suite.NotNil(suite.meta.GetPartition(ctx, partitionID))
		for _, segment := range segments {
			suite.NotNil(suite.targetMgr.GetSealedSegment(ctx, collection, segment, meta.CurrentTarget))
		}
	}
}

func (suite *JobSuite) assertCollectionReleased(collection int64) {
	ctx := context.Background()
	suite.False(suite.meta.Exist(ctx, collection))
	suite.Equal(0, len(suite.meta.ReplicaManager.GetByCollection(ctx, collection)))
	for _, channel := range suite.channels[collection] {
		suite.Nil(suite.targetMgr.GetDmChannel(ctx, collection, channel, meta.CurrentTarget))
	}
	for _, partitions := range suite.segments[collection] {
		for _, segment := range partitions {
			suite.Nil(suite.targetMgr.GetSealedSegment(ctx, collection, segment, meta.CurrentTarget))
		}
	}
}

func (suite *JobSuite) assertPartitionReleased(collection int64, partitionIDs ...int64) {
	ctx := context.Background()
	for _, partition := range partitionIDs {
		suite.Nil(suite.meta.GetPartition(ctx, partition))
		segments := suite.segments[collection][partition]
		for _, segment := range segments {
			suite.Nil(suite.targetMgr.GetSealedSegment(ctx, collection, segment, meta.CurrentTarget))
		}
	}
}

func (suite *JobSuite) updateSegmentDist(collection, node int64, partitions ...int64) {
	partitionSet := typeutil.NewSet(partitions...)
	metaSegments := make([]*meta.Segment, 0)
	for partition, segments := range suite.segments[collection] {
		if !partitionSet.Contain(partition) {
			continue
		}
		for _, segment := range segments {
			metaSegments = append(metaSegments,
				utils.CreateTestSegment(collection, partition, segment, node, 1, "test-channel"))
		}
	}
	suite.dist.SegmentDistManager.Update(node, metaSegments...)
}

func (suite *JobSuite) updateChannelDist(ctx context.Context, collection int64, loaded bool) {
	channels := suite.channels[collection]
	segments := lo.Flatten(lo.Values(suite.segments[collection]))

	replicas := suite.meta.ReplicaManager.GetByCollection(ctx, collection)
	targetVersion := suite.targetMgr.GetCollectionTargetVersion(ctx, collection, meta.CurrentTargetFirst)
	for _, replica := range replicas {
		if loaded {
			i := 0
			for _, node := range replica.GetNodes() {
				suite.dist.ChannelDistManager.Update(node, &meta.DmChannel{
					VchannelInfo: &datapb.VchannelInfo{
						CollectionID: collection,
						ChannelName:  channels[i],
					},
					Node: node,
					View: &meta.LeaderView{
						ID:           node,
						CollectionID: collection,
						Channel:      channels[i],
						Segments: lo.SliceToMap(segments, func(segment int64) (int64, *querypb.SegmentDist) {
							return segment, &querypb.SegmentDist{
								NodeID:  node,
								Version: time.Now().Unix(),
							}
						}),
						TargetVersion: targetVersion,
						Status: &querypb.LeaderViewStatus{
							Serviceable: true,
						},
					},
				})
				i++
				if i >= len(channels) {
					break
				}
			}
		} else {
			for _, node := range replica.GetNodes() {
				suite.dist.ChannelDistManager.Update(node)
			}
		}
	}
}

func (suite *JobSuite) TestLoadCollectionWithUserSpecifiedReplicaMode() {
	ctx := context.Background()

	// Test load collection with userSpecifiedReplicaMode = true
	for _, collection := range suite.collections {
		if suite.loadTypes[collection] != querypb.LoadType_LoadCollection {
			continue
		}

		req := &querypb.LoadCollectionRequest{
			CollectionID:  collection,
			ReplicaNumber: 1,
		}

		job := NewLoadCollectionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			true, // userSpecifiedReplicaMode = true
		)

		suite.scheduler.Add(job)
		err := job.Wait()
		suite.NoError(err)

		// Verify UserSpecifiedReplicaMode is set correctly
		loadedCollection := suite.meta.GetCollection(ctx, collection)
		suite.NotNil(loadedCollection)
		suite.True(loadedCollection.GetUserSpecifiedReplicaMode())

		suite.targetMgr.UpdateCollectionCurrentTarget(ctx, collection)
		suite.assertCollectionLoaded(collection)
	}
}

func (suite *JobSuite) TestLoadPartitionWithUserSpecifiedReplicaMode() {
	ctx := context.Background()

	// Test load partition with userSpecifiedReplicaMode = true
	for _, collection := range suite.collections {
		if suite.loadTypes[collection] != querypb.LoadType_LoadPartition {
			continue
		}

		req := &querypb.LoadPartitionsRequest{
			CollectionID:  collection,
			PartitionIDs:  suite.partitions[collection],
			ReplicaNumber: 1,
		}

		job := NewLoadPartitionJob(
			ctx,
			req,
			suite.dist,
			suite.meta,
			suite.broker,
			suite.targetMgr,
			suite.targetObserver,
			suite.collectionObserver,
			suite.nodeMgr,
			true, // userSpecifiedReplicaMode = true
		)

		suite.scheduler.Add(job)
		err := job.Wait()
		suite.NoError(err)

		// Verify UserSpecifiedReplicaMode is set correctly
		loadedCollection := suite.meta.GetCollection(ctx, collection)
		suite.NotNil(loadedCollection)
		suite.True(loadedCollection.GetUserSpecifiedReplicaMode())

		suite.targetMgr.UpdateCollectionCurrentTarget(ctx, collection)
		suite.assertCollectionLoaded(collection)
	}
}

func (suite *JobSuite) TestLoadPartitionUpdateUserSpecifiedReplicaMode() {
	ctx := context.Background()

	// First load partition with userSpecifiedReplicaMode = false
	collection := suite.collections[1] // Use partition load type collection
	if suite.loadTypes[collection] != querypb.LoadType_LoadPartition {
		return
	}

	req := &querypb.LoadPartitionsRequest{
		CollectionID:  collection,
		PartitionIDs:  suite.partitions[collection][:1], // Load first partition
		ReplicaNumber: 1,
	}

	job := NewLoadPartitionJob(
		ctx,
		req,
		suite.dist,
		suite.meta,
		suite.broker,
		suite.targetMgr,
		suite.targetObserver,
		suite.collectionObserver,
		suite.nodeMgr,
		false, // userSpecifiedReplicaMode = false
	)

	suite.scheduler.Add(job)
	err := job.Wait()
	suite.NoError(err)

	// Verify UserSpecifiedReplicaMode is false
	loadedCollection := suite.meta.GetCollection(ctx, collection)
	suite.NotNil(loadedCollection)
	suite.False(loadedCollection.GetUserSpecifiedReplicaMode())

	// Load another partition with userSpecifiedReplicaMode = true
	req2 := &querypb.LoadPartitionsRequest{
		CollectionID:  collection,
		PartitionIDs:  suite.partitions[collection][1:2], // Load second partition
		ReplicaNumber: 1,
	}

	job2 := NewLoadPartitionJob(
		ctx,
		req2,
		suite.dist,
		suite.meta,
		suite.broker,
		suite.targetMgr,
		suite.targetObserver,
		suite.collectionObserver,
		suite.nodeMgr,
		true, // userSpecifiedReplicaMode = true
	)

	suite.scheduler.Add(job2)
	err = job2.Wait()
	suite.NoError(err)

	// Verify UserSpecifiedReplicaMode is updated to true
	updatedCollection := suite.meta.GetCollection(ctx, collection)
	suite.NotNil(updatedCollection)
	suite.True(updatedCollection.GetUserSpecifiedReplicaMode())
}

func TestJob(t *testing.T) {
	suite.Run(t, new(JobSuite))
}

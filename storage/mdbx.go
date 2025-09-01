package storage

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/erigontech/mdbx-go/mdbx"
	"github.com/relab/hotstuff"
	"github.com/relab/hotstuff/eventloop"
	"github.com/relab/hotstuff/internal/proto/hotstuffpb"
	"github.com/relab/hotstuff/logging"
	"github.com/relab/hotstuff/modules"
	"google.golang.org/protobuf/proto"
)

const (
	// The name of the database file.
	dbFileName = "hotstuff.mdbx"
	// The name of the directory that contains the database file.
	dbDir = "/tmp/hotstuff-db"
)

var (
	// The name of the database that stores blocks.
	blocksDB = []byte("blocks")
	// The name of the database that stores metadata.
	metadataDB = []byte("metadata")
	// The key for the prune height value.
	pruneHeightKey = []byte("pruneHeight")
)

// mdbxChain implements the modules.BlockChain interface using MDBX.
type mdbxChain struct {
	env           *mdbx.Env
	logger        logging.Logger
	blocksDBI     mdbx.DBI
	metadataDBI   mdbx.DBI
	eventLoop     *eventloop.EventLoop
	configuration modules.Configuration
}

// NewMDBX creates a new blockchain implementation that uses MDBX.
func NewMDBX(id uint32) modules.BlockChain {
	log.Printf("Creating new MDBX blockchain for replica %d", id)
	err := os.MkdirAll(dbDir, 0755)
	if err != nil {
		panic(err)
	}

	env, err := mdbx.NewEnv(mdbx.Default)
	if err != nil {
		panic(err)
	}

	// TODO: make map size configurable
	err = env.SetGeometry(-1, -1, 1<<30, -1, -1, 4096) // 1 GB
	if err != nil {
		panic(err)
	}

	err = env.SetOption(mdbx.OptMaxDB, 2)
	if err != nil {
		panic(err)
	}

	dbPath := fmt.Sprintf("%s/hotstuff-%d.mdbx", dbDir, id)
	err = env.Open(dbPath, 0, 0644)
	if err != nil {
		panic(err)
	}

	mc := &mdbxChain{
		env: env,
	}

	err = env.Update(func(txn *mdbx.Txn) (err error) {
		mc.blocksDBI, err = txn.OpenDBI(string(blocksDB), mdbx.Create, nil, nil)
		if err != nil {
			return err
		}
		mc.metadataDBI, err = txn.OpenDBI(string(metadataDB), mdbx.Create, nil, nil)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		panic(err)
	}

	return mc
}

func (mc *mdbxChain) InitModule(mods *modules.Core) {
	mods.Get(&mc.logger, &mc.eventLoop, &mc.configuration)
}

// Store stores a block in the blockchain
func (mc *mdbxChain) Store(block *hotstuff.Block) {
	err := mc.env.Update(func(txn *mdbx.Txn) (err error) {
		// marshal the block to bytes
		pb := hotstuffpb.BlockToProto(block)
		b, err := proto.Marshal(pb)
		if err != nil {
			return err
		}
		// store the block
		hash := block.Hash()
		return txn.Put(mc.blocksDBI, hash[:], b, 0)
	})
	if err != nil {
		mc.logger.Errorf("Failed to store block in mdbx: %v", err)
	}
}

// LocalGet retrieves a block given its hash. It will only try the local cache.
func (mc *mdbxChain) LocalGet(hash hotstuff.Hash) (*hotstuff.Block, bool) {
	var block *hotstuff.Block
	err := mc.env.View(func(txn *mdbx.Txn) (err error) {
		// get the block bytes
		data, err := txn.Get(mc.blocksDBI, hash[:])
		if err != nil {
			return err
		}
		// unmarshal the block
		pb := new(hotstuffpb.Block)
		err = proto.Unmarshal(data, pb)
		if err != nil {
			return err
		}
		block = hotstuffpb.BlockFromProto(pb)
		return nil
	})
	if err != nil {
		if !mdbx.IsNotFound(err) {
			mc.logger.Errorf("Failed to get block from mdbx: %v", err)
		}
		return nil, false
	}
	return block, true
}

// Get retrieves a block given its hash. Get will try to find the block locally.
// If it is not available locally, it will try to fetch the block.
func (mc *mdbxChain) Get(hash hotstuff.Hash) (block *hotstuff.Block, ok bool) {
	block, ok = mc.LocalGet(hash)
	if ok {
		return block, true
	}
	// if the block is not in the db, we need to fetch it.
	ctx, cancel := context.WithTimeout(mc.eventLoop.Context(), 500*time.Millisecond) // Use a timeout to avoid waiting forever
	defer cancel()
	mc.logger.Debugf("Attempting to fetch block: %.8s", hash)
	block, ok = mc.configuration.Fetch(ctx, hash)
	if !ok {
		return nil, false
	}
	mc.Store(block)
	return block, true
}

// Extends checks if the given block extends the branch of the target block.
func (mc *mdbxChain) Extends(block, target *hotstuff.Block) bool {
	// TODO: implement
	return true
}

func (mc *mdbxChain) PruneToHeight(height hotstuff.View) (forkedBlocks []*hotstuff.Block) {
	// TODO: implement
	return nil
}

// Package mdbx provides an MDBX-backed implementation of the blockchain module.
package mdbx

import (
	"context"
	"encoding/binary"
	"sync"

	"github.com/erigontech/mdbx-go/mdbx"
	"github.com/relab/hotstuff"
	"github.com/relab/hotstuff/eventloop"
	"github.com/relab/hotstuff/internal/proto/hotstuffpb"
	"github.com/relab/hotstuff/logging"
	"github.com/relab/hotstuff/modules"
	"github.com/relab/hotstuff/synchronizer"
	"google.golang.org/protobuf/proto"
)

var (
	blocksBucket   = "blocks"
	heightBucket   = "blockAtHeight"
	metadataBucket = "metadata"
	pruneHeightKey = []byte("pruneHeight")
)

// MdbxBlockChain is a blockchain implementation that uses MDBX for storage.
type MdbxBlockChain struct {
	env *mdbx.Env

	blocksDBI mdbx.DBI
	heightDBI mdbx.DBI
	metaDBI   mdbx.DBI

	configuration modules.Configuration
	consensus     modules.Consensus
	eventLoop     *eventloop.EventLoop
	logger        logging.Logger

	mut          sync.Mutex
	pruneHeight  hotstuff.View
	pendingFetch map[hotstuff.Hash]context.CancelFunc // allows a pending fetch operation to be canceled
}

// NewMDBX returns a new MDBX-backed blockchain.
func NewMDBX() modules.BlockChain {
	return &MdbxBlockChain{
		pendingFetch: make(map[hotstuff.Hash]context.CancelFunc),
	}
}

// InitModule initializes the module.
func (chain *MdbxBlockChain) InitModule(mods *modules.Core) {
	var opts *modules.Options
	mods.Get(
		&chain.configuration,
		&chain.consensus,
		&chain.eventLoop,
		&chain.logger,
		&opts,
	)

	path := "hotstuff.mdbx"
	if opts.DBPath() != "" {
		path = opts.DBPath()
	}

	env, err := mdbx.NewEnv(mdbx.Default)
	if err != nil {
		chain.logger.Panicf("Failed to create mdbx environment: %v", err)
	}
	chain.env = env

	err = env.SetOption(mdbx.OptMaxDB, 3)
	if err != nil {
		chain.logger.Panicf("Failed to set max dbs: %v", err)
	}

	err = env.SetGeometry(-1, -1, 100*1024*1024, -1, -1, 4096) // 100MB
	if err != nil {
		chain.logger.Panicf("Failed to set mapsize: %v", err)
	}

	err = env.Open(path, 0, 0664)
	if err != nil {
		chain.logger.Panicf("Failed to open mdbx database at '%s': %v", path, err)
	}

	// Create buckets and get DBI handles
	err = env.Update(func(txn *mdbx.Txn) error {
		blocksDBI, err := txn.OpenDBI(blocksBucket, mdbx.Create, nil, nil)
		if err != nil {
			return err
		}
		chain.blocksDBI = blocksDBI

		heightDBI, err := txn.OpenDBI(heightBucket, mdbx.Create, nil, nil)
		if err != nil {
			return err
		}
		chain.heightDBI = heightDBI

		metaDBI, err := txn.OpenDBI(metadataBucket, mdbx.Create, nil, nil)
		if err != nil {
			return err
		}
		chain.metaDBI = metaDBI

		return nil
	})
	if err != nil {
		chain.logger.Panicf("Failed to create buckets: %v", err)
	}

	// Load pruneHeight from db.
	err = env.View(func(txn *mdbx.Txn) error {
		val, err := txn.Get(chain.metaDBI, pruneHeightKey)
		if err != nil {
			if mdbx.IsNotFound(err) {
				return nil // Not an error, just means it's not set yet.
			}
			return err
		}
		if len(val) > 0 {
			chain.pruneHeight = hotstuff.View(binary.LittleEndian.Uint64(val))
		}
		return nil
	})
	if err != nil {
		chain.logger.Panicf("Failed to load prune height: %v", err)
	}

	// Store genesis block if it's not already there.
	_, ok := chain.LocalGet(hotstuff.GetGenesis().Hash())
	if !ok {
		chain.Store(hotstuff.GetGenesis())
	}
}

// Store stores a block in the blockchain.
func (chain *MdbxBlockChain) Store(block *hotstuff.Block) {
	chain.mut.Lock()
	defer chain.mut.Unlock()

	err := chain.env.Update(func(txn *mdbx.Txn) error {
		// store block
		pb := hotstuffpb.BlockToProto(block)
		blockBytes, err := proto.Marshal(pb)
		if err != nil {
			return err
		}

		hash := block.Hash()
		err = txn.Put(chain.blocksDBI, hash[:], blockBytes, 0)
		if err != nil {
			return err
		}

		// store height -> hash mapping
		var heightBytes [8]byte
		binary.LittleEndian.PutUint64(heightBytes[:], uint64(block.View()))
		err = txn.Put(chain.heightDBI, heightBytes[:], hash[:], 0)
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		chain.logger.Errorf("Failed to store block: %v", err)
		return
	}

	// cancel any pending fetch operations
	if cancel, ok := chain.pendingFetch[block.Hash()]; ok {
		cancel()
		delete(chain.pendingFetch, block.Hash())
	}
}

// LocalGet retrieves a block given its hash, without fetching it from other replicas.
func (chain *MdbxBlockChain) LocalGet(hash hotstuff.Hash) (*hotstuff.Block, bool) {
	var block *hotstuff.Block
	err := chain.env.View(func(txn *mdbx.Txn) error {
		blockBytes, err := txn.Get(chain.blocksDBI, hash[:])
		if err != nil {
			return err
		}

		pb := new(hotstuffpb.Block)
		err = proto.Unmarshal(blockBytes, pb)
		if err != nil {
			return err
		}

		block = hotstuffpb.BlockFromProto(pb)
		return nil
	})

	if err != nil {
		if !mdbx.IsNotFound(err) {
			chain.logger.Errorf("Failed to get block from mdbx: %v", err)
		}
		return nil, false
	}

	return block, true
}

// Get retrieves a block given its hash. Get will try to find the block locally.
// If it is not available locally, it will try to fetch the block.
func (chain *MdbxBlockChain) Get(hash hotstuff.Hash) (block *hotstuff.Block, ok bool) {
	if block, ok = chain.LocalGet(hash); ok {
		return block, true
	}

	chain.mut.Lock()
	if _, fetching := chain.pendingFetch[hash]; fetching {
		chain.mut.Unlock()
	} else {
		chain.mut.Unlock() // unlock before fetching
	}

	ctx, cancel := synchronizer.TimeoutContext(chain.eventLoop.Context(), chain.eventLoop)

	chain.mut.Lock()
	chain.pendingFetch[hash] = cancel
	chain.mut.Unlock()

	chain.logger.Debugf("Attempting to fetch block: %.8s", hash)
	fetchedBlock, ok := chain.configuration.Fetch(ctx, hash)

	chain.mut.Lock()
	delete(chain.pendingFetch, hash)
	chain.mut.Unlock()

	if !ok {
		return chain.LocalGet(hash)
	}

	chain.logger.Debugf("Successfully fetched block: %.8s", fetchedBlock.Hash())
	chain.Store(fetchedBlock)

	return fetchedBlock, true
}

// Extends checks if the given block extends the branch of the target block.
func (chain *MdbxBlockChain) Extends(block, target *hotstuff.Block) bool {
	current := block
	ok := true
	for ok && current.View() > target.View() {
		current, ok = chain.Get(current.Parent())
	}
	return ok && current.Hash() == target.Hash()
}

// PruneToHeight prunes blocks from the database up to the specified height.
func (chain *MdbxBlockChain) PruneToHeight(height hotstuff.View) (forkedBlocks []*hotstuff.Block) {
	chain.mut.Lock()
	defer chain.mut.Unlock()

	committedHeight := chain.consensus.CommittedBlock().View()
	committedViews := make(map[hotstuff.View]bool)
	committedViews[committedHeight] = true

	err := chain.env.View(func(txn *mdbx.Txn) error {
		for h := committedHeight; h >= chain.pruneHeight; {
			var heightBytes [8]byte
			binary.LittleEndian.PutUint64(heightBytes[:], uint64(h))
			hashBytes, err := txn.Get(chain.heightDBI, heightBytes[:])
			if err != nil {
				if mdbx.IsNotFound(err) {
					break
				}
				return err
			}
			var hash hotstuff.Hash
			copy(hash[:], hashBytes)
			block, ok := chain.LocalGet(hash)
			if !ok {
				break
			}
			parent, ok := chain.LocalGet(block.Parent())
			if !ok || parent.View() < chain.pruneHeight {
				break
			}
			h = parent.View()
			committedViews[h] = true
		}
		return nil
	})
	if err != nil {
		chain.logger.Errorf("Failed to build committed views for pruning: %v", err)
		return nil
	}

	err = chain.env.Update(func(txn *mdbx.Txn) error {
		for h := height; h > chain.pruneHeight; h-- {
			if !committedViews[h] {
				var heightBytes [8]byte
				binary.LittleEndian.PutUint64(heightBytes[:], uint64(h))
				hashBytes, err := txn.Get(chain.heightDBI, heightBytes[:])
				if err != nil {
					if mdbx.IsNotFound(err) {
						continue
					}
					return err
				}
				var hash hotstuff.Hash
				copy(hash[:], hashBytes)
				block, ok := chain.LocalGet(hash)
				if ok {
					chain.logger.Debugf("PruneToHeight: found forked block: %v", block)
					forkedBlocks = append(forkedBlocks, block)
				}
			}
			var heightBytes [8]byte
			binary.LittleEndian.PutUint64(heightBytes[:], uint64(h))
			err := txn.Del(chain.heightDBI, heightBytes[:], nil)
			if err != nil && !mdbx.IsNotFound(err) {
				return err
			}
		}
		var pruneHeightBytes [8]byte
		binary.LittleEndian.PutUint64(pruneHeightBytes[:], uint64(height))
		return txn.Put(chain.metaDBI, pruneHeightKey, pruneHeightBytes[:], 0)
	})
	if err != nil {
		chain.logger.Errorf("Failed to prune: %v", err)
	}

	chain.pruneHeight = height
	return forkedBlocks
}

var _ modules.BlockChain = (*MdbxBlockChain)(nil)

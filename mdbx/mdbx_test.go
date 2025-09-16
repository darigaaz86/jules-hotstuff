package mdbx

import (
	"io/ioutil"
	"os"
	"testing"

	"github.com/relab/hotstuff"
	"github.com/relab/hotstuff/internal/testutil"
	"github.com/relab/hotstuff/modules"
	"go.uber.org/mock/gomock"
)

func newTestMDBX(t *testing.T) (*MdbxBlockChain, func()) {
	t.Helper()

	tmpDir, err := ioutil.TempDir("", "hotstuff-mdbx-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	ctrl := gomock.NewController(t)
	builder := &modules.Builder{}
	testutil.TestModules(t, ctrl, 1, nil, builder, nil)

	opts := &modules.Options{}
	opts.SetDBPath(tmpDir)

	builder.Add(opts)

	mods := builder.Build()

	chain := NewMDBX().(*MdbxBlockChain)
	chain.InitModule(mods)

	return chain, func() {
		chain.env.Close()
		os.RemoveAll(tmpDir)
	}
}

func TestStoreAndGet(t *testing.T) {
	chain, closer := newTestMDBX(t)
	defer closer()

	block := hotstuff.NewBlock(hotstuff.GetGenesis().Hash(), hotstuff.NewQuorumCert(nil, 0, hotstuff.GetGenesis().Hash()), "test", 1, 1)

	chain.Store(block)

	retrievedBlock, ok := chain.LocalGet(block.Hash())
	if !ok {
		t.Fatal("Failed to retrieve block")
	}

	if retrievedBlock.Hash() != block.Hash() {
		t.Errorf("Hashes don't match: want: %s, got: %s", block.Hash(), retrievedBlock.Hash())
	}
}

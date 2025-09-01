package hotstuffpb

import (
	"math/big"

	"github.com/relab/hotstuff"
	"github.com/relab/hotstuff/crypto"
	"github.com/relab/hotstuff/crypto/bls12"
	"github.com/relab/hotstuff/crypto/ecdsa"
	"github.com/relab/hotstuff/crypto/eddsa"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// BlockToProto converts a hotstuff.Block to a hotstuffpb.Block.
func BlockToProto(block *hotstuff.Block) *Block {
	parentHash := block.Parent()
	return &Block{
		Parent:    parentHash[:],
		Proposer:  uint32(block.Proposer()),
		Command:   []byte(block.Command()),
		QC:        QuorumCertToProto(block.QuorumCert()),
		View:      uint64(block.View()),
		Timestamp: timestamppb.New(block.Timestamp()),
	}
}

// BlockFromProto converts a hotstuffpb.Block to a hotstuff.Block.
func BlockFromProto(pb *Block) *hotstuff.Block {
	var parentHash hotstuff.Hash
	copy(parentHash[:], pb.GetParent())
	b := hotstuff.NewBlock(
		parentHash,
		QuorumCertFromProto(pb.GetQC()),
		hotstuff.Command(pb.GetCommand()),
		hotstuff.View(pb.GetView()),
		hotstuff.ID(pb.GetProposer()),
	)
	b.SetTimestamp(pb.GetTimestamp().AsTime())
	return b
}

// QuorumCertToProto converts a hotstuff.QuorumCert to a hotstuffpb.QuorumCert.
func QuorumCertToProto(qc hotstuff.QuorumCert) *QuorumCert {
	hash := qc.BlockHash()
	return &QuorumCert{
		Sig:  QuorumSignatureToProto(qc.Signature()),
		View: uint64(qc.View()),
		Hash: hash[:],
	}
}

// QuorumCertFromProto converts a hotstuffpb.QuorumCert to a hotstuff.QuorumCert.
func QuorumCertFromProto(pb *QuorumCert) hotstuff.QuorumCert {
	var hash hotstuff.Hash
	copy(hash[:], pb.GetHash())
	return hotstuff.NewQuorumCert(
		QuorumSignatureFromProto(pb.GetSig()),
		hotstuff.View(pb.GetView()),
		hash,
	)
}

// QuorumSignatureToProto converts a hotstuff.QuorumSignature to a hotstuffpb.QuorumSignature.
func QuorumSignatureToProto(sig hotstuff.QuorumSignature) *QuorumSignature {
	if sig == nil {
		return nil
	}
	switch s := sig.(type) {
	case crypto.Multi[*ecdsa.Signature]:
		sigs := make([]*ECDSASignature, 0, len(s))
		for id, sig := range s {
			sigs = append(sigs, &ECDSASignature{
				Signer: uint32(id),
				R:      sig.R().Bytes(),
				S:      sig.S().Bytes(),
			})
		}
		return &QuorumSignature{
			Sig: &QuorumSignature_ECDSASigs{
				ECDSASigs: &ECDSAMultiSignature{
					Sigs: sigs,
				},
			},
		}
	case *bls12.AggregateSignature:
		return &QuorumSignature{
			Sig: &QuorumSignature_BLS12Sig{
				BLS12Sig: &BLS12AggregateSignature{
					Sig:          s.ToBytes(),
					Participants: s.Bitfield().Bytes(),
				},
			},
		}
	case crypto.Multi[*eddsa.Signature]:
		sigs := make([]*EDDSASignature, 0, len(s))
		for id, sig := range s {
			sigs = append(sigs, &EDDSASignature{
				Signer: uint32(id),
				Sig:    sig.ToBytes(),
			})
		}
		return &QuorumSignature{
			Sig: &QuorumSignature_EDDSASigs{
				EDDSASigs: &EDDSAMultiSignature{
					Sigs: sigs,
				},
			},
		}
	default:
		return &QuorumSignature{}
	}
}

// QuorumSignatureFromProto converts a hotstuffpb.QuorumSignature to a hotstuff.QuorumSignature.
func QuorumSignatureFromProto(pb *QuorumSignature) hotstuff.QuorumSignature {
	if pb == nil {
		return nil
	}
	switch s := pb.GetSig().(type) {
	case *QuorumSignature_ECDSASigs:
		sigs := make(crypto.Multi[*ecdsa.Signature])
		for _, sig := range s.ECDSASigs.GetSigs() {
			r := new(big.Int)
			r.SetBytes(sig.GetR())
			s := new(big.Int)
			s.SetBytes(sig.GetS())
			sigs[hotstuff.ID(sig.GetSigner())] = ecdsa.RestoreSignature(r, s, hotstuff.ID(sig.GetSigner()))
		}
		return sigs
	case *QuorumSignature_BLS12Sig:
		bf := crypto.BitfieldFromBytes(s.BLS12Sig.GetParticipants())
		sig, err := bls12.RestoreAggregateSignature(s.BLS12Sig.GetSig(), bf)
		if err != nil {
			return nil
		}
		return sig
	case *QuorumSignature_EDDSASigs:
		sigs := make(crypto.Multi[*eddsa.Signature])
		for _, sig := range s.EDDSASigs.GetSigs() {
			sigs[hotstuff.ID(sig.GetSigner())] = eddsa.RestoreSignature(sig.GetSig(), hotstuff.ID(sig.GetSigner()))
		}
		return sigs
	default:
		return nil
	}
}

// PartialCertToProto converts a hotstuff.PartialCert to a hotstuffpb.PartialCert.
func PartialCertToProto(pc hotstuff.PartialCert) *PartialCert {
	hash := pc.BlockHash()
	return &PartialCert{
		Sig:  QuorumSignatureToProto(pc.Signature()),
		Hash: hash[:],
	}
}

// PartialCertFromProto converts a hotstuffpb.PartialCert to a hotstuff.PartialCert.
func PartialCertFromProto(pb *PartialCert) hotstuff.PartialCert {
	var hash hotstuff.Hash
	copy(hash[:], pb.GetHash())
	return hotstuff.NewPartialCert(
		QuorumSignatureFromProto(pb.GetSig()),
		hash,
	)
}

// TimeoutCertToProto converts a hotstuff.TimeoutCert to a hotstuffpb.TimeoutCert.
func TimeoutCertToProto(tc hotstuff.TimeoutCert) *TimeoutCert {
	return &TimeoutCert{
		Sig:  QuorumSignatureToProto(tc.Signature()),
		View: uint64(tc.View()),
	}
}

// TimeoutCertFromProto converts a hotstuffpb.TimeoutCert to a hotstuff.TimeoutCert.
func TimeoutCertFromProto(pb *TimeoutCert) hotstuff.TimeoutCert {
	return hotstuff.NewTimeoutCert(
		QuorumSignatureFromProto(pb.GetSig()),
		hotstuff.View(pb.GetView()),
	)
}

// TimeoutMsgToProto converts a hotstuff.TimeoutMsg to a hotstuffpb.TimeoutMsg.
func TimeoutMsgToProto(tm hotstuff.TimeoutMsg) *TimeoutMsg {
	return &TimeoutMsg{
		View:     uint64(tm.View),
		SyncInfo: SyncInfoToProto(tm.SyncInfo),
		MsgSig:   QuorumSignatureToProto(tm.MsgSignature),
		ViewSig:  QuorumSignatureToProto(tm.ViewSignature),
	}
}

// TimeoutMsgFromProto converts a hotstuffpb.TimeoutMsg to a hotstuff.TimeoutMsg.
func TimeoutMsgFromProto(pb *TimeoutMsg) hotstuff.TimeoutMsg {
	return hotstuff.TimeoutMsg{
		View:          hotstuff.View(pb.GetView()),
		SyncInfo:      SyncInfoFromProto(pb.GetSyncInfo()),
		MsgSignature:  QuorumSignatureFromProto(pb.GetMsgSig()),
		ViewSignature: QuorumSignatureFromProto(pb.GetViewSig()),
	}
}

// SyncInfoToProto converts a hotstuff.SyncInfo to a hotstuffpb.SyncInfo.
func SyncInfoToProto(si hotstuff.SyncInfo) *SyncInfo {
	qc, hasQC := si.QC()
	tc, hasTC := si.TC()
	aggQC, hasAggQC := si.AggQC()

	var pbQC *QuorumCert
	if hasQC {
		pbQC = QuorumCertToProto(qc)
	}

	var pbTC *TimeoutCert
	if hasTC {
		pbTC = TimeoutCertToProto(tc)
	}

	var pbAggQC *AggQC
	if hasAggQC {
		pbAggQC = AggQCToProto(aggQC)
	}

	return &SyncInfo{
		QC:    pbQC,
		TC:    pbTC,
		AggQC: pbAggQC,
	}
}

// SyncInfoFromProto converts a hotstuffpb.SyncInfo to a hotstuff.SyncInfo.
func SyncInfoFromProto(pb *SyncInfo) hotstuff.SyncInfo {
	si := hotstuff.NewSyncInfo()
	if pb.GetQC() != nil {
		si = si.WithQC(QuorumCertFromProto(pb.GetQC()))
	}
	if pb.GetTC() != nil {
		si = si.WithTC(TimeoutCertFromProto(pb.GetTC()))
	}
	if pb.GetAggQC() != nil {
		si = si.WithAggQC(AggQCFromProto(pb.GetAggQC()))
	}
	return si
}

// ProposalToProto converts a hotstuff.ProposeMsg to a hotstuffpb.Proposal.
func ProposalToProto(p hotstuff.ProposeMsg) *Proposal {
	return &Proposal{
		Block: BlockToProto(p.Block),
	}
}

// ProposalFromProto converts a hotstuffpb.Proposal to a hotstuff.ProposeMsg.
func ProposalFromProto(p *Proposal) hotstuff.ProposeMsg {
	return hotstuff.ProposeMsg{
		Block: BlockFromProto(p.GetBlock()),
	}
}

// AggQCToProto converts a hotstuff.AggregateQC to a hotstuffpb.AggQC.
func AggQCToProto(aggQC hotstuff.AggregateQC) *AggQC {
	qcs := make(map[uint32]*QuorumCert)
	for id, qc := range aggQC.QCs() {
		qcs[uint32(id)] = QuorumCertToProto(qc)
	}
	return &AggQC{
		QCs:  qcs,
		Sig:  QuorumSignatureToProto(aggQC.Sig()),
		View: uint64(aggQC.View()),
	}
}

// AggQCFromProto converts a hotstuffpb.AggQC to a hotstuff.AggregateQC.
func AggQCFromProto(pb *AggQC) hotstuff.AggregateQC {
	qcs := make(map[hotstuff.ID]hotstuff.QuorumCert)
	for id, qc := range pb.GetQCs() {
		qcs[hotstuff.ID(id)] = QuorumCertFromProto(qc)
	}
	return hotstuff.NewAggregateQC(
		qcs,
		QuorumSignatureFromProto(pb.GetSig()),
		hotstuff.View(pb.GetView()),
	)
}

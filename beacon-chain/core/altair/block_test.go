package altair_test

import (
	"testing"

	types "github.com/prysmaticlabs/eth2-types"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/altair"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	p2pType "github.com/prysmaticlabs/prysm/beacon-chain/p2p/types"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/params"
	altairTest "github.com/prysmaticlabs/prysm/shared/testutil/altair"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

func TestProcessSyncCommittee_OK(t *testing.T) {
	beaconState, privKeys := altairTest.DeterministicGenesisStateAltair(t, params.BeaconConfig().MaxValidatorsPerCommittee)
	require.NoError(t, beaconState.SetSlot(1))
	committee, err := altair.SyncCommittee(beaconState, helpers.CurrentEpoch(beaconState))
	require.NoError(t, err)
	require.NoError(t, beaconState.SetCurrentSyncCommittee(committee))

	syncBits := bitfield.NewBitvector1024()
	for i := range syncBits {
		syncBits[i] = 0xff
	}
	beaconState.RotateAttestations()
	indices, err := altair.SyncCommitteeIndices(beaconState, helpers.CurrentEpoch(beaconState))
	require.NoError(t, err)
	ps := helpers.PrevSlot(beaconState.Slot())
	pbr, err := helpers.BlockRootAtSlot(beaconState, ps)
	require.NoError(t, err)
	sigs := make([]bls.Signature, len(indices))
	for i, indice := range indices {
		b := p2pType.SSZBytes(pbr)
		sb, err := helpers.ComputeDomainAndSign(beaconState, helpers.CurrentEpoch(beaconState), &b, params.BeaconConfig().DomainSyncCommittee, privKeys[indice])
		require.NoError(t, err)
		sig, err := bls.SignatureFromBytes(sb)
		require.NoError(t, err)
		sigs[i] = sig
	}
	aggregatedSig := bls.AggregateSignatures(sigs).Marshal()
	syncAggregate := &ethpb.SyncAggregate{
		SyncCommitteeBits:      syncBits,
		SyncCommitteeSignature: aggregatedSig,
	}

	beaconState, err = altair.ProcessSyncCommittee(beaconState, syncAggregate)
	require.NoError(t, err)

	// Use a non-sync committee index to compare profitability.
	syncCommittee := make(map[types.ValidatorIndex]bool)
	for _, index := range indices {
		syncCommittee[index] = true
	}
	nonSyncIndex := types.ValidatorIndex(params.BeaconConfig().MaxValidatorsPerCommittee + 1)
	for i := types.ValidatorIndex(0); uint64(i) < params.BeaconConfig().MaxValidatorsPerCommittee; i++ {
		if !syncCommittee[i] {
			nonSyncIndex = i
			break
		}
	}

	// Sync committee should be more profitable than non sync committee
	balances := beaconState.Balances()
	require.Equal(t, true, balances[indices[0]] > balances[nonSyncIndex])

	// Proposer should be more profitable than rest of the sync committee
	proposerIndex, err := helpers.BeaconProposerIndex(beaconState)
	require.NoError(t, err)
	require.Equal(t, true, balances[proposerIndex] > balances[indices[0]])

	// Sync committee should have the same profits, except you are a proposer
	for i := 1; i < len(indices); i++ {
		if proposerIndex == indices[i-1] || proposerIndex == indices[i] {
			continue
		}
		require.Equal(t, balances[indices[i-1]], balances[indices[i]])
	}

	// Increased balance validator count should equal to sync committee count
	increased := uint64(0)
	for _, balance := range balances {
		if balance > params.BeaconConfig().MaxEffectiveBalance {
			increased++
		}
	}
	require.Equal(t, params.BeaconConfig().SyncCommitteeSize, increased)
}
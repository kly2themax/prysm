package rpc

import (
	"context"
	"fmt"

	"github.com/gogo/protobuf/proto"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/state"
	v "github.com/prysmaticlabs/prysm/beacon-chain/core/validators"
	"github.com/prysmaticlabs/prysm/beacon-chain/db"
	pbp2p "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	pb "github.com/prysmaticlabs/prysm/proto/beacon/rpc/v1"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/hashutil"
)

// ProposerServer defines a server implementation of the gRPC Proposer service,
// providing RPC endpoints for computing state transitions and state roots, proposing
// beacon blocks to a beacon node, and more.
type ProposerServer struct {
	beaconDB           *db.BeaconDB
	chainService       chainService
	powChainService    powChainService
	canonicalStateChan chan *pbp2p.BeaconState
}

// ProposerIndex sends a response to the client which returns the proposer index for a given slot. Validators
// are shuffled and assigned slots to attest/propose to. This method will look for the validator that is assigned
// to propose a beacon block at the given slot.
func (ps *ProposerServer) ProposerIndex(ctx context.Context, req *pb.ProposerIndexRequest) (*pb.ProposerIndexResponse, error) {
	beaconState, err := ps.beaconDB.State()
	if err != nil {
		return nil, fmt.Errorf("could not get beacon state: %v", err)
	}

	proposerIndex, err := v.BeaconProposerIdx(
		beaconState,
		req.SlotNumber,
	)
	if err != nil {
		return nil, fmt.Errorf("could not get index of previous proposer: %v", err)
	}

	return &pb.ProposerIndexResponse{
		Index: proposerIndex,
	}, nil
}

// ProposeBlock is called by a proposer in a sharding validator and a full beacon node
// sends the request into a beacon block that can then be included in a canonical chain.
func (ps *ProposerServer) ProposeBlock(ctx context.Context, blk *pbp2p.BeaconBlock) (*pb.ProposeResponse, error) {
	h, err := hashutil.HashBeaconBlock(blk)
	if err != nil {
		return nil, fmt.Errorf("could not hash block: %v", err)
	}
	log.WithField("blockHash", fmt.Sprintf("%#x", h)).Debugf("Block proposal received via RPC")
	// We relay the received block from the proposer to the chain service for processing.
	ps.chainService.IncomingBlockFeed().Send(blk)
	return &pb.ProposeResponse{BlockHash: h[:]}, nil
}

// ComputeStateRoot computes the state root after a block has been processed through a state transition and
// returns it to the validator client.
func (ps *ProposerServer) ComputeStateRoot(ctx context.Context, req *pbp2p.BeaconBlock) (*pb.StateRootResponse, error) {
	beaconState, err := ps.beaconDB.State()
	if err != nil {
		return nil, fmt.Errorf("could not get beacon state: %v", err)
	}

	parentHash := bytesutil.ToBytes32(req.ParentRootHash32)
	beaconState, err = state.ExecuteStateTransition(
		beaconState,
		req,
		parentHash,
		false, /* no sig verification */
	)
	if err != nil {
		return nil, fmt.Errorf("could not execute state transition %v", err)
	}

	encodedState, err := proto.Marshal(beaconState)
	if err != nil {
		return nil, fmt.Errorf("could not marshal state %v", err)
	}

	// TODO(#1389): Use tree hashing algorithm instead.
	beaconStateHash := hashutil.Hash(encodedState)
	log.WithField("beaconStateHash", fmt.Sprintf("%#x", beaconStateHash)).Debugf("Computed state hash")
	return &pb.StateRootResponse{
		StateRoot: beaconStateHash[:],
	}, nil
}

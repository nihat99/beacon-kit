// SPDX-License-Identifier: MIT
//
// Copyright (c) 2024 Berachain Foundation
//
// Permission is hereby granted, free of charge, to any person
// obtaining a copy of this software and associated documentation
// files (the "Software"), to deal in the Software without
// restriction, including without limitation the rights to use,
// copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the
// Software is furnished to do so, subject to the following
// conditions:
//
// The above copyright notice and this permission notice shall be
// included in all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND,
// EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES
// OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
// NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT
// HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY,
// WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
// FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
// OTHER DEALINGS IN THE SOFTWARE.

package preblock

import (
	"cosmossdk.io/log"
	"github.com/berachain/beacon-kit/mod/primitives"
	"github.com/berachain/beacon-kit/mod/runtime/abci"
	abcitypes "github.com/berachain/beacon-kit/mod/runtime/abci/types"
	"github.com/berachain/beacon-kit/mod/runtime/services/blockchain"
	cometabci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BeaconPreBlockHandler is responsible for aggregating oracle data from each
// validator and writing the oracle data into the store before any transactions
// are executed/finalized for a given block.
type BeaconPreBlockHandler struct {
	// cfg is the configuration for block proposals and finalization.
	cfg *abci.Config

	// logger is the logger used by the handler.
	logger log.Logger

	// chainService is the service that is responsible for interacting with
	// the beacon chain.
	chainService *blockchain.Service

	// nextHandler is the next pre-block handler in the chain. This is always
	// nesting of the next pre-block handler into this handler.
	nextHandler sdk.PreBlocker
}

// NewBeaconPreBlockHandler returns a new BeaconPreBlockHandler. The handler
// is responsible for writing oracle data included in vote extensions to state.
func NewBeaconPreBlockHandler(
	cfg *abci.Config,
	logger log.Logger,
	chainService *blockchain.Service,
	nextHandler sdk.PreBlocker,
) *BeaconPreBlockHandler {
	return &BeaconPreBlockHandler{
		cfg:          cfg,
		logger:       logger,
		chainService: chainService,
		nextHandler:  nextHandler,
	}
}

// PreBlocker is called by the base app before the block is finalized. It
// is responsible for aggregating oracle data from each validator and writing
// the oracle data to the store.
func (h *BeaconPreBlockHandler) PreBlocker() sdk.PreBlocker {
	return func(
		ctx sdk.Context, req *cometabci.RequestFinalizeBlock,
	) error {
		// Process the Slot.
		if err := h.chainService.ProcessSlot(ctx); err != nil {
			h.logger.Error("failed to process slot", "error", err)
			return err
		}

		// Extract the beacon block from the ABCI request.
		//
		// TODO: Block factory struct?
		// TODO: Use protobuf and .(type)?
		blk, err := abcitypes.ReadOnlyBeaconBlockFromABCIRequest(
			req,
			h.cfg.BeaconBlockPosition,
			h.chainService.BeaconCfg().ActiveForkVersionForSlot(
				primitives.Slot(req.Height),
			),
		)
		if err != nil {
			return err
		}

		blobSideCars, err := abcitypes.GetBlobSideCars(
			req, h.cfg.BlobSidecarsBlockPosition,
		)
		if err != nil {
			return err
		}

		// Processing the incoming beacon block and blobs.
		cacheCtx, write := ctx.CacheContext()
		if err = h.chainService.ProcessBeaconBlock(
			cacheCtx,
			blk,
			blobSideCars,
		); err != nil {
			h.logger.Warn(
				"failed to receive beacon block",
				"error",
				err,
			)
			// TODO: Emit Evidence so that the validator can be slashed.
		} else {
			// We only want to persist state changes if we successfully
			// processed the block.
			write()
		}

		// Process the finalization of the beacon block.
		if err = h.chainService.PostBlockProcess(
			ctx, blk,
		); err != nil {
			return err
		}

		// Call the nested child handler.
		return h.callNextHandler(ctx, req)
	}
}

// callNextHandler calls the next pre-block handler in the chain.
func (h *BeaconPreBlockHandler) callNextHandler(
	ctx sdk.Context, req *cometabci.RequestFinalizeBlock,
) error {
	// If there is no child handler, we are done, this preblocker
	// does not modify any consensus params so we return an empty
	// response.
	if h.nextHandler == nil {
		return nil
	}

	return h.nextHandler(ctx, req)
}

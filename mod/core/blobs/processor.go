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

package blobs

import (
	"context"
	"errors"

	"github.com/berachain/beacon-kit/mod/core"
	"github.com/berachain/beacon-kit/mod/core/types"
	"github.com/berachain/beacon-kit/mod/da"
	datypes "github.com/berachain/beacon-kit/mod/da/types"
	"github.com/berachain/beacon-kit/mod/primitives"
	"github.com/sourcegraph/conc/iter"
	"golang.org/x/sync/errgroup"
)

// Processor is the processor for blobs.
type Processor struct {
	bv *da.BlobVerifier
}

// NewProcessor creates a new processor.
func NewProcessor(bv *da.BlobVerifier) *Processor {
	return &Processor{
		bv: bv,
	}
}

// ProcessBlob processes a blob.
func (p *Processor) ProcessBlobs(
	slot primitives.Slot,
	avs core.AvailabilityStore,
	sidecars *datypes.BlobSidecars,
) error {
	g, _ := errgroup.WithContext(context.Background())

	// Verify the inclusion proofs on the blobs.
	g.Go(func() error {
		if err := errors.Join(iter.Map(
			sidecars.Sidecars,
			func(sidecar **datypes.BlobSidecar) error {
				sc := *sidecar
				if sc == nil {
					return ErrAttemptedToVerifyNilSidecar
				}

				// Verify the KZG inclusion proof.
				return types.VerifyKZGInclusionProof(sc)
			},
		)...); err != nil {
			return err
		}
		return nil
	})

	// Verify the KZG proofs on the blobs.
	g.Go(func() error {
		return p.bv.VerifyKZGProofs(sidecars)
	})

	// Wait for the goroutines to finish.
	if err := g.Wait(); err != nil {
		return err
	}

	// Persist the blobs to the availability store.
	return avs.Persist(slot, sidecars)
}
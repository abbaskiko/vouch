// Copyright © 2020 Attestant Limited.
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package best

import (
	"context"
	"sync"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	"golang.org/x/sync/semaphore"
)

// BeaconBlockProposal provies the best beacon block proposal from a number of beacon nodes.
func (s *Service) BeaconBlockProposal(ctx context.Context, slot uint64, randaoReveal []byte, graffiti []byte) (*spec.BeaconBlock, error) {
	var mu sync.Mutex
	bestScore := float64(0)
	var bestProposal *spec.BeaconBlock

	sem := semaphore.NewWeighted(s.processConcurrency)
	var wg sync.WaitGroup
	for name, provider := range s.beaconBlockProposalProviders {
		wg.Add(1)
		go func(ctx context.Context, sem *semaphore.Weighted, wg *sync.WaitGroup, name string, provider eth2client.BeaconBlockProposalProvider, mu *sync.Mutex) {
			defer wg.Done()

			if err := sem.Acquire(ctx, 1); err != nil {
				log.Error().Err(err).Msg("Failed to acquire semaphore")
				return
			}
			defer sem.Release(1)
			log := log.With().Str("provider", name).Uint64("slot", slot).Logger()

			opCtx, cancel := context.WithTimeout(ctx, s.timeout)
			started := time.Now()
			proposal, err := provider.BeaconBlockProposal(opCtx, slot, randaoReveal, graffiti)
			s.clientMonitor.ClientOperation(name, "beacon block proposal", err == nil, time.Since(started))
			if err != nil {
				log.Warn().Err(err).Msg("Failed to obtain beacon block proposal")
				cancel()
				return
			}
			log.Trace().Dur("elapsed", time.Since(started)).Msg("Obtained beacon block proposal")
			cancel()

			mu.Lock()
			score := scoreBeaconBlockProposal(ctx, name, proposal)
			if score > bestScore || bestProposal == nil {
				bestScore = score
				bestProposal = proposal
			}
			mu.Unlock()
		}(ctx, sem, &wg, name, provider, &mu)
	}
	wg.Wait()

	return bestProposal, nil
}

package eth2_shuffle_experiment

import "encoding/binary"

type HashFn func(input []byte) []byte

const hSeedSize = int8(32)
const hRoundSize = int8(1)
const hPositionWindowSize = int8(4)
const hPivotViewSize = hSeedSize + hRoundSize
const hTotalSize = hSeedSize + hRoundSize + hPositionWindowSize


// Shuffles the list
func ShuffleList(hashFn HashFn, input []uint64, rounds uint8, seed [32]byte) {
	innerShuffleList(hashFn, input, rounds, seed, true)
}

// Un-shuffles the list
func UnshuffleList(hashFn HashFn, input []uint64, rounds uint8, seed [32]byte) {
	innerShuffleList(hashFn, input, rounds, seed, false)
}

// Shuffles or unshuffles, depending on the `dir` (true for shuffling, false for unshuffling
func innerShuffleList(hashFn HashFn, input []uint64, rounds uint8, seed [32]byte, dir bool) {
	if len(input) <= 1 {
		// nothing to (un)shuffle
		return
	}
	if rounds == 0 {
		return
	}
	// The new version uses the inverse, to make writes nicely consecutive
	dir = !dir
	listSize := uint64(len(input))
	buf := make([]byte, hTotalSize, hTotalSize)
	r := uint8(0)
	if !dir {
		// Start at last round.
		// Iterating through the rounds in reverse, un-swaps everything, effectively un-shuffling the list.
		r = rounds - 1
	}

	// Seed is always the first 32 bytes of the hash input, we never have to change this part of the buffer.
	copy(buf[:hSeedSize], seed[:])

	pivots := make([]uint64, rounds, rounds)

	// pre-compute the pivots
	{
		// Divide rounds by 4, round up the number
		pivotHashes := (rounds + 3) / 4
		for i := uint8(0); i < pivotHashes; i++ {
			// pivot_hash_offset = 8*(round % 4)
			// pivot = bytes_to_int(hash(seed + int_to_bytes1(round / 4))[pivot_hash_offset:pivot_hash_offset+8]) % list_size
			// This is the "int_to_bytes1(round)", appended to the seed.
			buf[hSeedSize] = i
			// compute 1 hash (32 bytes) to derive 4 pivots (each 8 bytes) from (or less, clip end)
			// Seed is already in place, now just hash the correct part of the buffer (Seed bytes, pivot byte), and take a uint64 from it,
			h := hashFn(buf[:hPivotViewSize])[:8]
			for j, x := 4*i, 0; j < rounds && x < 32; j, x = j+1, x+8 {
				// 64 bit number, no big deal to use modulo here (insignificant bias)
				pivots[j] = binary.LittleEndian.Uint64(h[x:x+8]) % listSize
			}
		}
	}

	// pre-compute the hashes

	// we have n/2 pairs (if odd; one of the mirror points is simply unpaired,
	//  and doesn't need a pair bit, it's still shuffled during different pivot choices)
	pairs := listSize / 2
	// the number of swap-or-not bytes spent per validator
	widthBytes := (uint64(rounds) + 7) / 8
	pairsPerHash := 32 / widthBytes
	if pairsPerHash == 0 {
		panic("too many rounds")
	}
	// swap-or-not, per pair
	swapOrNot := make([]byte, pairs*widthBytes, pairs*widthBytes)
	for i := uint64(0); i < pairs; i++ {
		// TODO: we use 64 bit nums for validator indices / pairs, yet we only consider 32 bits for the unique hash.
		//  (same in old spec shuffling). It's unrealistic to expect more than 4M validators however, so in practice it doesn't matter
		binary.LittleEndian.PutUint32(buf[hPivotViewSize:], uint32(i))
		source := hashFn(buf)
		// example, 90 rounds: 90+38, 90+38
		for j := uint64(0); j < pairsPerHash; j++ {
			start := j*widthBytes
			copy(swapOrNot[i*widthBytes:], source[start:start+widthBytes])
		}
	}

	output := make([]uint64, len(input), len(input))
	for i := uint64(0); i < listSize; i++ {
		x := i
		for {
			pivot := pivots[r]
			// spec: flip = (pivot - index) % list_size
			// Add extra list_size to prevent underflows.
			// "flip" will be the other side of the pair
			flip := (pivot + (listSize - x)) % listSize
			// ignore a mirror point that swaps with itself
			if flip != x {
				// lowest indexes the pairs by two series: 0...mirror_1, pivot...mirror_2
				lowest := x
				if flip < x {
					lowest = flip
				}
				// pair indexes as one consecutive series
				pair := lowest
				if lowest > pivot {
					pair -= pivot / 2
					// If the pivot is odd, reduce one more
					//  (the first mirror point swaps with itself, and is not considered a pair)
					if pivot&1 == 1 {
						pair--
					}
				}
				// get the byte corresponding to this round
				byteI := (pair * widthBytes) + uint64(r/8)
				byteV := swapOrNot[byteI]
				// get the bit within the byte corresponding to this round
				bitV := (byteV >> (r & 7)) & 1
				// if the bit is 1, we flip
				if bitV == 1 {
					x = flip
				}
			}

			// go forwards?
			if dir {
				// -> shuffle
				r++
				if r == rounds {
					break
				}
			} else {
				if r == 0 {
					break
				}
				// -> un-shuffle
				r--
			}
		}
		// for i, use the unshuffled index, get the original validator index for this, and write it to the output
		output[i] = input[x]
	}
	// not really in-place computation anymore, but just testing now.
	copy(input, output)
}

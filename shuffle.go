package eth2_shuffle_experiment

import "encoding/binary"

type HashFn func(input []byte) []byte

const hSeedSize = int8(32)
const hPivotSize = int8(32 + 1)
const hPairSize = int8(32 + 4)
const hMaxSize = hPairSize


// Shuffles the list
func ShuffleList(hashFn HashFn, input []uint64, roundsPow uint8, seed [32]byte) {
	innerShuffleList(hashFn, input, roundsPow, seed, true)
}

// Un-shuffles the list
func UnshuffleList(hashFn HashFn, input []uint64, roundsPow uint8, seed [32]byte) {
	innerShuffleList(hashFn, input, roundsPow, seed, false)
}

// Shuffles or unshuffles, depending on the `dir` (true for shuffling, false for unshuffling
// rounds = 2**roundsPow, max roundsPow = 8
func innerShuffleList(hashFn HashFn, input []uint64, roundsPow uint8, seed [32]byte, dir bool) {
	if len(input) <= 1 {
		// nothing to (un)shuffle
		return
	}
	if roundsPow == 0 {
		return
	}
	if roundsPow > 8 {
		panic("too many rounds")
	}
	// The new version uses the inverse, to make writes nicely consecutive
	dir = !dir
	listSize := uint64(len(input))
	if listSize > uint64(1) << 32 {
		panic("input list too large")
	}
	buf := make([]byte, hMaxSize, hMaxSize)
	rounds := uint64(1) << roundsPow

	// Seed is always the first 32 bytes of the hash input, we never have to change this part of the buffer.
	copy(buf[:hSeedSize], seed[:])

	pivots := make([]uint64, rounds, rounds)

	// pre-compute the pivots
	{
		// compute (rounds/4) hashes to derive pivots from (4 8-byte hashes per pivot, i.e. 64 bits)
		pivotHashes := uint8(1)
		if roundsPow > 2 {
			pivotHashes <<= roundsPow - 2
		}
		for i := uint8(0); i < pivotHashes; i++ {
			// pivot = bytes_to_int(hash(seed + int_to_bytes1(i))[pivot_hash_offset:pivot_hash_offset+8]) % list_size
			// This is the "int_to_bytes1(round)", appended to the seed.
			buf[hSeedSize] = i
			// Seed is already in place, now just hash the correct part of the buffer (Seed bytes, pivot byte), and take a uint64 from it,
			h := hashFn(buf[:hPivotSize])

			// clip if there's less pivots necessary than you can get from a single hash
			for p := uint8(0); p < 4 && rounds > uint64(p); p++ {
				pivots[(i << 2) + p] = binary.LittleEndian.Uint64(h[p << 3:(p + 1) << 3]) % listSize
			}
		}
	}

	// pre-compute the hashes
	var swapOrNot []byte

	{
		// we have n/2 pairs (if odd; one of the mirror points is simply unpaired,
		//  and doesn't need a pair bit, it's still shuffled during different pivot choices)
		pairs := listSize >> 1
		widthBytes := rounds >> 3
		// support small amount of rounds
		if widthBytes == 0 {
			widthBytes = 1
		}
		pairsPerHash := 32 / widthBytes
		// round up amount of hashes necessary to cover every pair
		hashes := (pairs + pairsPerHash - 1) / pairsPerHash
		// swap-or-not, per pair.
		swapOrNot = make([]byte, hashes<<(8-3), hashes<<(8-3))
		offset := uint64(0)
		for i := uint64(0); i < hashes; i++ {
			// You could expand hash input to 64 bits for (gigantic) sets of validators. 32 bits is sufficient.
			binary.LittleEndian.PutUint32(buf[hSeedSize:hPairSize], uint32(i))
			source := hashFn(buf)
			//source := []byte{0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55, 0x55}
			copy(swapOrNot[offset:offset+32], source)
			offset += 32
		}
	}

	output := make([]uint64, len(input), len(input))
	for i := uint64(0); i < listSize; i++ {
		x := i
		r := uint64(0)
		if !dir {
			// Start at last round.
			// Iterating through the rounds in reverse, un-swaps everything, effectively un-shuffling the list.
			r = rounds - 1
		}
		for {

			pivot := pivots[r]
			// spec (watch out, signed math): flip = (pivot - index) % list_size
			// "flip" will be the other side of the pair
			var flip uint64
			var m uint64
			if x <= pivot {
				// flip around mirror_0
				m = (pivot + 1) >> 1
				if x > m {
					// flip from right to left
					flip = m - (x - m)
				} else if x < m {
					// flip from left to right
					flip = m + (m - x) - 1
				}
			} else {
				// flip around mirror_1
				m = (pivot + listSize + 1) >> 1
				if x > m {
					// flip from right to left
					flip = m - (x - m)
				} else if x < m {
					// flip from left to right
					flip = m + (m - x) - 1
				}
			}

			// if it's not flipping with itself
			if m != x {

				// lowest indexes the pairs by two series: 0...mirror_1, pivot...mirror_2
				lowest := x
				if flip < x {
					lowest = flip
				}
				// pair indexes as one consecutive series
				pair := lowest
				if lowest >= pivot {
					pair -= pivot >> 1
					// If the pivot is odd, reduce one more
					//  (the first mirror point swaps with itself, and is not considered a pair)
					if pivot&1 == 1 {
						pair--
					}
				}
				// get the byte corresponding to this round.
				// Simply multiple pair with the widthBytes (no need for mul op), to find the offset of the swapOrNot bytes for the given pair.
				// Then add round/8 to determine the byte that contains the bit for the current round.
				byteI := (pair << (roundsPow - 3)) + uint64(r>>3)
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

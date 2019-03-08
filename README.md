# ETH 2.0 Shuffle experiment

This is an experimental idea to try and improve the ETH 2.0 shuffling performance.
Shuffling in ETH 2.0 is already sort of efficient.
See my other implementation [eth2-shuffle](https://github.com/protolambda/eth2-shuffle).
4 million items shuffling takes ~1.2 seconds,

However, there is room to improve if we change the shuffling algorithm a little bit, 
 but keep same idea and sought-for properties (permute-index, and reversal).

## The goal

- Improve permute-index, because why not?
- Reduce writes, they take too long.
- Make shuffling easy to run as tasks in short-lived parallel tasks (e.g. go-routine)
- Consider L1/L2/L3 caches

A.k.a. try to win a > 2x improvement on current performance, possibly > 10x if you consider parallelization.

## The idea

Shuffling is currently a NxNx90 bitfield 
ranks: 
1: lookup index i
2: destination given pivot at col j
3: repeat 90 times (ignore pair optimization for a moment)

I.e.: for every round (90x), you go through every validator (N) and swap it with 1/N other validators (i.e. an arbitrary write, hence you think of it as an dimension).
This swap however, requires you to write memory, in an arbitrary location (because of random pivot).

What we need: a Nx90 bitfield, for any stage in shuffling, we ask swap or not? *But we don't immediately swap*
Instead, similar to a permute-index call, we fully shuffle a validator index, and only then continue to the next.

However, with a bitfield like this, we don't have to hash as much:
if a validator index is not shuffled (i.e. "swap-or-not -> not", or index is a mirror-point),
 we can decide to continue reading data from the same hash. 

We don't take a different hash for each round, we take a different hash for each position. 
And the hash is long enough to support 90 rounds (1 bit per round).

Also note that hashes are independent from pivots; swapping is still a question with a random 50% Yes answer. 

The pairing problem: *validator shuffling is pair-dependent,
 we have to consider the same bit for both sides of the swap-pair.

It would be wonderful if we can keep reading from a single hash for the shuffling of a validator,
 but at some point a bit in the hash says "swap-or-not -> swap", and we change position with our pair-counterpart.
In this case we need to start reading from a different hash.
Luckily, hashes are not so much data to pre-compute,
 and reads are very cheap for data that's in the L1/L2 CPU data cache. And L3 is doable.

The benefit here is that we do not have to write during the computation of the swapping. Only at the very finish.
A 90x reduction in writes!

### Algorithm

1. Allocate a shuffling result array that will hold the new shuffling
1. Precompute a N/2 * 90 bitfield.
    - We want 1 hash to be used by at least 1 pair position; with a 256 bit hash we can support two 90 bit swap bitfields.
    - We want 1 pair position to use at most 1 hash: if we swap 100% of the time, we touch at most 90 hashes, not more. On average it will be 45 hashes,
     an improvement over the current permute-index requirement of 90 swap-or-not choice hashes.
    - Keep data as minimal as possible, we want it to fit in L1 + L2 data cache. And fallback on L3 in worst case.
2. Precompute 90 pivots.
    - Pivot = 64 bit index
    - Calculate pivots using random input from a few hashes.
      Extract 90 64-bit numbers: `90*64/256=22.5` -> round up to 23 hashes.
      (Each pivot fits in 32 bits also, but we need a larger number to prevent modulo bias when adjusting the pivot to `N`) 
3. Iterate through each index `i`, from `0...N` (excl., validator count)
    - copy `i` to `j`, this will be the shuffle output. Keep `j` in prioritized write-speed location (if possible).
    - Iterate through each pivot, `p` from `89...0` (yes, reverse, we want to back track the original index of a shuffled outcome,
      or we redefine this to make it 0...89, and do the reverse for un-shuffling, as long as that's consistent)
        - We lookup the byte with the bit `b` for index `j`, at pivot `p`. (if we do not already have the byte from a previous round)
        - if bit `b` is 0: we don't swap, yeah, continue to `p-1` for free, possibly using the same byte to retrieve the next bit from.
        - if bit `b` is 1: we swap. Now we change `j` to the pairing of the swap (similar to previous permute-index `flip` index): `j = (p - j) % N`. Next read will be from a different byte, but we're not writing anything arbitrary yet,
            just one `j` that is being written to, but fits in a register anyway, no need to worry about write speed there.
    - We've arrived at the end, now we can write `new_shuffling[i] = old_shuffling[j]`

Amount of hashes: all hashes are done in pre-computation, but there are not that many:

1. pivots only require 23 hashes (`23*256>90*64`)
2. "swap or not" bitfields are 90 bits each. Because it applies to pairs, we only need N/2 90 bit bitfields.
 And a 256 bit hash can accommodate two 90 bitfields (with room to spare, we could up the rounds number for extra security without to much cost).
3. Note that because we're not filling the n/2*90 bitfield with continuous hashing, but keep hashing aligned. 
This way we only need to compute 90 hashes for permute-index if we swap every time. Avg. case will be 45 hashes for swap-decisions.

## TLDR:

- `23 + N/4` hashes for shuffle-list, instead of previous `90*N/2/256=N/5.689` hashes. Worse, but 90x less writes.
- `23 + 90` hashes for permute index worst case, `23 + 45` hashes avg. case. Instead of previous `90 + 90` hashes.

## Trade-offs

The trade-off is basically:

Pro new approach:
- **90 times less writes**
- All writes are either to a local 64 bit variable, or to consecutive array values, not arbitrary locations.
- The data being read is small and fits within L2 in many cases. L3 for worst-case validator amounts, also ok.
 Certainly since we'll have bigger L1/L2 and/or quicker L3 by the hypothetical time we reach such a validator number.
- Expected `180/(23+45)=2.6` times better permute-index performance. (not critical, but nice)
- **We can run it in parallel as much as we want** (shared memory for reads, no write conflicts) by splitting the work of shuffling N validators between C cores. After precomputation (which can be parallelized too).
 I.e. each takes `i` from the for loop out of a `N/C` partition.

Con new approach:
- `5.689/4=42%` more hashes. But a SHA-256 hash doesn't take that long, since it's 
A: optimized by processor. and/or B: works with not to many write locations, i.e. L1 or higher speed (guess).
- Bit more complex to understand
- Some languages/targets do not benefit much from this.
- *untested*/*not benchmarked* (later work)


## Concerns

- Hashing: do we want less or more rounds, we could change it to 64, 128, 256.
- Pre-compute size: we want to make it as minimal as possible to make the bitfield fit in memory cache.
 90 bits does not align well, it would be 12 bytes per validator. Aka. 48 MB worst case (4M validators). 
 But if it fits in L2 (~2-4 MB?) and/or L3 (~2-20 MB?) we should get some pretty good speeds (up to 1M validators ?).
- Due to this "transposing" of the hashing, and effectively making the position of 4 validators dependent on 
 90 pivots + 1 swap-or-not hash, it may be easier to exploit. (although limited by seed randomness anyway)

## Extensions

- We could try to make the **pre-compute structure** more intelligent; optimize for less arbitrary reads. 
 Possibly by copying data (although we want it to be close to CPU).
- Instead of doing 90 rounds consecutively, we could also **repeat a shuffle of less rounds** a few times.
Every time is N more writes, but memory requirements scale down, making it more feasible to run from **just L1/L2 data**.

## Plans

Experimental implementation(s) coming later, patience please.
This is hobby work, and have much more ETH 2.0 hobby work (Go executable ETH 2.0 spec!),
 and a limited amount of free time.


## License

MIT, see license file

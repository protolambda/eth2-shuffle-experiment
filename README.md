# ETH 2.0 Shuffle experiment

Warning;

This was an experimental idea to try and improve the ETH 2.0 shuffling performance.
Turns out that it's infeasible to use a more permute-index alike approach, because of random access reads of larger data portions being to slow.

**Hashing and writing the outputs are not necessarily the only bottlenecks**: when you are working with a large pre-computed list of data, you have to deal with slow reads.





## License

MIT, see license file

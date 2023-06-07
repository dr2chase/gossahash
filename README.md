# gossahash-search
Searches for the function that the SSA phase of the Go compiler is doing wrong.

```
Usage of ./gossahash:
  -BX
        for repeated multi-point failure search, exclude all points on failure location
  -E string
        prefix string for environment-encoded variables, e.g., GOCOMPILEDEBUG= or GODEBUG= (default "GOCOMPILEDEBUG=")
  -F    act as a test program.  Generates multiple multipoint failures.
  -H string
        string prepended to all hash encodings, for special hash interpretation/debugging
  -R string
        begin searching at this suffix, it should known-fail for this suffix[1:]
  -X string
        exclude these suffixes from matching
  -e string
        name/prefix of variable communicating hash suffix (default "gossahash")
  -f    if set, use a file instead of standard out for hash trigger information
  -fma
        search for fused-multiply-add floating point rounding problems (for arm64, ppc64, s390x)
  -n int
        stop after finding this many failures (0 for don't stop) (default 1)
  -t int
        timeout in seconds for running test script, 0=run till done. Negative timeout means timing out is a pass, not a failure (default 900)
  -v    also print output of test script (default false)
```

./gossahash runs the test executable (default ./gshs_test.bash) repeatedly
with longer and longer hash suffix parameters supplied. A non-default
command and args can be specified following any flags or "--".  For
example, if the a compiler change has broken the build and the change
has been gated with a hash (see below), 
```
	gossahash ./make.bash
```
will search for a function whose miscompilation causes the problem.

The hash suffix is made of 1 and 0 characters, expected to match the
suffix of a hash of something interesting, like a function or variable
name or their combination. Each run of the executable is expected to
print '\<evname\> triggered' (for example, 'gossahash triggered') and the hash
suffix(es) are chosen to search for the one(s) that result in a single
trigger line occurring.  Multiple occurrences of exactly the same
trigger line are counted once.

By default the trigger lines are expected to be written to standard
output, but -f flag sets the environment variable GSHS_LOGFILE to
name a file where the test command *may* write its logging output.
This permits use with test harnesses that swallow standard
output and/or expect not to see "trigger" chit-chat.  Note that
any tests or builds using "-f" should run in a series of
single processes, and not in several running at the same time,
else they may overwrite the logfile.  Similarly, the programs
that are debugged using GSHS_LOGFILE should open it in append
mode, not truncate, since they may have been preceded by some
other phase of the build or test.

The ./gossahash command can be run as its own test with the -F flag, as in
(prints about 100 long lines, and demonstrates multi-point failure detection):
```
  ./gossahash ./gossahash -F
```

The compiler-side version of this protocol has become more complicated
over time to provide support for "multiple-point" failure and detection
of multiple failures.  The code in `fail.go` can be used for this purpose.

# gossahash-search
Searches for the function that the SSA phase of the Go compiler is doing wrong.

```
Usage of ./gossahash:
  -F	act as a test program
  -P string
    	root of hash suffix to begin searching at (default empty)
  -X	swap pass and fail for test script (default false)
  -e string
    	name/prefix of environment variable communicating hash suffix (default "GOSSAHASH")
  -f	use file for 'triggered' communication (sets GSHS_LOGFILE)
  -l string
    	prefix of log file names ending ...{PASS,FAIL}.log (default "GSHS_LAST_")
  -n int
    	maximum hash suffix length to try before giving up (default 30)
  -s	use stdout for 'triggered' communication (obsolete, now default) (default true)
  -t int
    	timeout in seconds for running test script, 0=run till done. Negative timeout means timing out is a pass, not a failure (default 900)
  -v	also print output of test script (default false)
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
print '\<evname\> triggered' (for example, 'GOSSAHASH triggered') and the hash
suffix(es) are chosen to search for the one(s) that result in a single
trigger line occurring.  Multiple occurrences of exactly the same
trigger line are counted once.  When fewer than 4 lines trigger, the
matching trigger lines are included in the output.

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

Swapping pass and fail can be used to selectively disable the
minimum number of optimizations to allow the code to run.

The ./gossahash command can be run as its own test with the -F flag, as in
(prints about 100 long lines, and demonstrates multi-point failure detection):
```
  ./gossahash ./gossahash -F
```
This Go code can be used to trigger the tested behavior:

```
func doit(name string) bool {
    if os.Getenv("GOSSAHASH") == "" {
        return true  // Default behavior is yes.
    }
    // Check hash of name against a partial input hash.  We use this feature
    // to do a binary search to find a function that is incorrectly compiled.
    hstr := ""
    for _, b := range sha1.Sum([]byte(name)) {
        hstr += fmt.Sprintf("%08b", b)
    }
    if strings.HasSuffix(hstr, os.Getenv("GOSSAHASH")) {
        fmt.Printf("GOSSAHASH triggered %s\n", name)
        return true
    }
    // Iteratively try additional hashes to allow tests for multi-point failure.
    for i := 0; true; i++ {
        ev := fmt.Sprintf("GOSSAHASH%d", i)
        evv := os.Getenv(ev)
        if evv == "" {
            break
        }
        if strings.HasSuffix(hstr, evv) {
            fmt.Printf("%s triggered %s\n", ev, name)
            return true
        }
    }
    return false
}
```
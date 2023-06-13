// Copyright 2018 Google LLC

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at

//     https://www.apache.org/licenses/LICENSE-2.0

// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	hashLimit      int    = 30 // Maximum length of a hash string
	test_command   string = "./gshs_test.bash"
	initialSuffix  string = ""           // The initial hash suffix assumed to cause failure.
	restartSuffix  string = ""           // Restart a search here.
	restartExclude string = ""           // Exclude these suffixes from search (comma or minus separated).
	logPrefix      string = "GSHS_LAST_" // Prefix on PASS/FAIL log files.
	verbose        bool   = false
	timeout        int    = 900 // Timeout in seconds to apply to command; failure if hit
	multiple       int    = 1   // Search for this many failures.
	seed           int64  = time.Now().UnixNano()
	batchExclude   bool   = false
	bisectSyntax   bool   = false

	// Name of the environment variable that contains the hash suffix to be matched against function name hashes.
	hash_ev_string = "gossahash"
	hash_ev_name   = "needs to be set"
	// Expect to see this in the output when a value for gossahash triggers SSA-compilation of a function.
	function_selection_string     string
	function_selection_logfile    string
	function_selection_use_stdout bool = true  // Use stdout instead of a file (now default, old flag)
	function_selection_use_file   bool = false // Use file instead of stdout

	commandLineEnv []string // environment variables supplied on command line after flags, before command.

	tmpdir string

	fail bool // If true, converts behavior to a test program
)

const (
	FAILED  = iota // Script exited with return code > 0 and multiple functions SSA compiled.
	DONE           // Script exited with return code > 0 and exactly one function SSA compiled.
	DONE0          // Script exited with return code > 0 and no functions SSA compiled (means test is flaky)
	PASSED         // Script exited with return code 0
	PASSED0        // Script exited with return code 0 AND no functions SSA compiled.
)

// saveLogFiles stores data in filename, unless it cannot
// in which case it whines (but still returns).
// The default permission on the file name is conservative
// because "you never know".
func saveLogFile(filename string, data []byte) {
	error := ioutil.WriteFile(filename, data, 0600)
	if error != nil {
		fmt.Fprintf(os.Stderr, "Error saving log file %s\n", error)
	}
}

type arg []string

var args arg = arg{test_command} // default value for -h printing, will be discarded.

func (a *arg) String() string {
	return fmt.Sprintf("%v", *a)
}

func (a *arg) Set(value string) error {
	*a = append([]string(*a), value)
	return nil
}

var excludes []string // hashes already seen to fail, now excluded

type searchState struct {
	suffix string

	// The accumulated list of hashes that are either proven
	// singleton triggers that contribute to failure, or proven/
	// inferred to trigger at least one SSA-compilation that
	// contributes to failure.
	hashes []string

	// hashes before  this index correspond to a single function
	// whose compilation is necessary to trigger a failure.
	// This counter advances as new singleton-triggering hashes
	// are found.
	next_singleton_hash_index int

	lastTrigger     string
	lastOutput      []byte
	withoutExcludes bool // initially, false == "with excludes"
}

var initialEnvEnvPrefix = "GOCOMPILEDEBUG="

var envEnvPrefix = initialEnvEnvPrefix

// hashPrefix is a string that precedes the hashcodes, for signalling
// different sorts of hashing (e.g., full path name vs basename)
var hashPrefix = ""

var sep = "/"

func (ss *searchState) newStyleEnvString(withExcludes bool) string {
	ev := fmt.Sprintf("%s%s=%s", envEnvPrefix, hash_ev_string, hashPrefix)
	if withExcludes {
		for _, x := range excludes {
			ev += "-" + x + sep
		}
	}
	ev += ss.suffix
	for i := 0; i < len(ss.hashes); i++ {
		ev += fmt.Sprintf("%s%s", sep, ss.hashes[i])
	}
	return ev
}

// tryCmd runs the test command with suffix and all the hashes
// assigned to environment variables of the form GOSSAHASH and
// GOSSAHASH%d for [0:len(hashes)-1]
// If timeout is greater than zero then the command will be
// killed after that many seconds (to help with bugs that exhibit
// as an infinite loop), otherwise it runs to completion and the
// error code and output are captured and returned.
func (ss *searchState) tryCmd(suffix string) (output []byte, err error) {
	cmd := exec.Command(test_command)
	cmd.Args = append(cmd.Args, args...)

	// Fill the env
	cmd.Env = os.Environ()
	extraEnv := make([]string, 0)

	if function_selection_logfile != "" {
		// Create and truncate the file, then inject it into the environment
		f, _ := os.Create(function_selection_logfile)

		f.Close()
		ev := fmt.Sprintf("%s=%s", "GSHS_LOGFILE", function_selection_logfile)
		extraEnv = append(extraEnv, ev)
	}

	extraEnv = append(extraEnv, ss.newStyleEnvString(!ss.withoutExcludes))

	extraEnv = append(extraEnv, commandLineEnv...)

	cmd.Env = append(cmd.Env, extraEnv...)

	if verbose || true {
		line := ""
		for _, e := range extraEnv {
			line += e
			line += " "
		}
		line += test_command
		for _, a := range args {
			line += " "
			line += a
		}

		fmt.Fprintf(os.Stdout, "Trying: %s\n", line)
	} else {
		if len(extraEnv) == 0 {
			fmt.Fprintf(os.Stdout, "Trying %s\n", suffix)
		} else {
			fmt.Fprintf(os.Stdout, "Trying %s\n", extraEnv)
		}
	}

	if timeout == 0 {
		output, err = cmd.CombinedOutput()
	} else {
		var b bytes.Buffer
		cmd.Stdout = &b
		cmd.Stderr = &b
		err = cmd.Start()
		if err != nil {
			return
		}
		var killErr error
		var timedOut bool
		var timeoutMeansPass bool
		t := timeout
		if timeout < 0 {
			timeoutMeansPass = true
			t = -timeout
		}
		doneChan := make(chan int, 1)
		timer := time.AfterFunc(time.Second*time.Duration(t), func() {
			timedOut = true
			p := cmd.Process
			killErr = p.Signal(os.Interrupt)
			for i := 0; i < 100; i++ {
				time.Sleep(time.Millisecond * 250)
				select {
				case <-doneChan:
					return
				default:
				}
			}
			killErr = p.Signal(os.Kill)
		})
		err = cmd.Wait()
		doneChan <- 1
		if killErr != nil {
			// Not sure what I would do with this,
			// and it could appear merely as the result of a lost race.
		}
		timer.Stop()
		output = b.Bytes()
		if timedOut {
			status := "fail"
			if timeoutMeansPass {
				err = nil
				status = "pass"
			}
			fmt.Fprintf(os.Stdout, "Timeout after %d seconds (%s): ", t, status)
		}
	}

	if verbose {
		fmt.Fprintf(os.Stdout, "%s", string(output))
	}
	return
}

var hashmatch = regexp.MustCompilePOSIX("[01]+|0x[0-9a-f]+")

// matchTrigger extracts hash trigger reports from the output.
// repeats are collapsed, but counted in the returned map.  The
// last match is also returned.
func matchTrigger(output []byte, hash_ev_name, suffix string) (map[string]int, string) {

	mask := uint64(1)<<len(suffix) - 1
	suffixVal, _ := strconv.ParseUint(suffix, 2, 64)
	suffixVal &= mask

	triggerPrefix := hash_ev_name + " triggered"
	if bisectSyntax {
		triggerPrefix = "[bisect-match "
	}

	m := make(map[string]int)
	var lastTrigger string
	scanner := bufio.NewScanner(bytes.NewBuffer(output))
	for scanner.Scan() {
		s := strings.TrimSpace(scanner.Text())
		if pi := strings.Index(s, triggerPrefix); pi != -1 {
			var space int
			end := -1
			if bisectSyntax {
				// [bisect-match 0xabcd]
				space = strings.LastIndex(s, " ")
				end = strings.LastIndex(s, "]")
			}
			if end == -1 {
				space = strings.LastIndex(s, " ")
				end = len(s)
			}

			if space == -1 {
				space = len(s)
				m[s] = m[s] + 1
			} else {
				h := strings.TrimSpace(s[space:end])
				if ss := hashmatch.FindStringSubmatch(h); len(ss) == 1 && ss[0] == h {
					if bisectSyntax {
						// Suffix must match
						hv, err := strconv.ParseUint(h[2:], 16, 64)
						if err == nil {
							if hv&mask != suffixVal {
								continue
							}
							m[h] = m[h] + 1
						} else {
							panic(fmt.Errorf("Failed to parse %s, error %v", h[2:], err))
						}
					} else {
						m[h] = m[h] + 1
					}
				} else {
					m[s] = m[s] + 1
				}
			}
			if bisectSyntax {
				lastTrigger = strings.TrimSpace(s[0:pi])
			} else {
				lastTrigger = strings.TrimSpace(s[len(triggerPrefix):space])
			}

		}
	}
	return m, lastTrigger
}

func parseExcludes(x string) []string {
	if x == "" {
		return nil
	}
	var xs []string
	var a string
	for _, c := range x {
		switch c {
		case '0', '1':
			a = a + string(c)
		case ' ', ',', '-', '+':
			if len(a) > 0 {
				xs = append(xs, a)
				a = ""
			}
		}
	}
	if len(a) > 0 {
		xs = append(xs, a)
	}
	return xs
}

// trySuffix runs the test command passing it suffix as an argument,
// and returns PASSED/FAILED/DONE/DONE0 based on return code and occurrences
// of the function_selection_string within the output; if there is only
// one and the command fails, then the search is done.
// Appropriate log files and narrative are also produced.
func (ss *searchState) trySuffix(suffix string) (int, []byte) {
	ss.suffix = suffix
	output, error := ss.tryCmd(suffix)

	if function_selection_logfile != "" {
		outputf, errorf := ioutil.ReadFile(function_selection_logfile)
		if errorf == nil {
			output = outputf
		}
	}

	// Compilations sometimes occur more than once, so stuff the
	// matching string into a map. Note the map contains the whole
	// line, so varying output not included in the hash can prevent
	// convergence on a single trigger line.

	var m map[string]int
	m, ss.lastTrigger = matchTrigger(output, hash_ev_name, suffix)
	count := len(m)

	// (error == nil) means success
	prefix := ""

	if error != nil {
		why := error.Error()
		// we like errors.
		fmt.Fprintf(os.Stdout, "%s %sfailed (%d distinct triggers): %s\n", test_command, prefix, count, why)
		lfn := fmt.Sprintf("%s%sFAIL.%d.log", logPrefix, prefix, ss.next_singleton_hash_index)
		// lfn = filepath.Join(tmpdir, lfn)
		saveLogFile(lfn, output)
		if count <= 1 {
			fmt.Fprintf(os.Stdout, "Review %s for %sfailing run\n", lfn, prefix)
			if count == 0 {
				return DONE0, output
			}
			return DONE, output
		}
		return FAILED, output
	}
	saveLogFile(logPrefix+prefix+"PASS.log", output)
	if count == 0 {
		return PASSED0, output
	}
	return PASSED, output
}

func main() {
	fma := false
	loopvar := false

	flag.BoolVar(&batchExclude, "BX", batchExclude, "for repeated multi-point failure search, exclude all points on failure location")
	flag.StringVar(&initialEnvEnvPrefix, "E", initialEnvEnvPrefix, "prefix string for environment-encoded variables, e.g., GOCOMPILEDEBUG= or GODEBUG=")
	flag.BoolVar(&fail, "F", fail, "act as a test program.  Generates multiple multipoint failures.")
	flag.StringVar(&hashPrefix, "H", hashPrefix, "string prepended to all hash encodings, for special hash interpretation/debugging")
	flag.StringVar(&restartSuffix, "R", restartSuffix, "begin searching at this suffix, it should known-fail for this suffix[1:]")
	flag.StringVar(&restartExclude, "X", restartExclude, "exclude these suffixes from matching")
	flag.BoolVar(&bisectSyntax, "B", bisectSyntax, "use bisect syntax for matches")

	flag.StringVar(&hash_ev_string, "e", hash_ev_string, "name/prefix of variable communicating hash suffix")
	flag.BoolVar(&function_selection_use_file, "f", function_selection_use_file, "if set, use a file instead of standard out for hash trigger information")
	flag.BoolVar(&fma, "fma", fma, "search for fused-multiply-add floating point rounding problems (for arm64, ppc64, s390x)")
	flag.BoolVar(&loopvar, "loopvar", loopvar, "search for loopvar-dependent failures")
	flag.IntVar(&multiple, "n", multiple, "stop after finding this many failures (0 for don't stop)")
	flag.IntVar(&timeout, "t", timeout, "timeout in seconds for running test script, 0=run till done. Negative timeout means timing out is a pass, not a failure")
	flag.BoolVar(&verbose, "v", verbose, "also print output of test script (default false)")

	// flag.StringVar(&logPrefix, "l", logPrefix, "prefix of log file names ending ...{PASS,FAIL}.log")

	// flag.BoolVar(&function_selection_use_stdout, "s", function_selection_use_stdout, "use stdout for 'triggered' communication (obsolete, now default)")
	// flag.BoolVar(&function_selection_use_file, "f", function_selection_use_file, "use file for 'triggered' communication (sets GSHS_LOGFILE)")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr,
			`
%s runs the test executable (default ./gshs_test.bash) repeatedly 
with longer and longer hash suffix parameters supplied. A non-default
command and args can be specified following any flags or "--".  For
example, if the a compiler change has broken the build and the change
has been gated with a hash (see below), 
	gossahash ./make.bash
will search for a function whose miscompilation causes the problem.

The hash suffix is made of 1 and 0 characters, expected to match the
suffix of a hash of something interesting, like a function or variable
name or their combination. Each run of the executable is expected to
print '<evname> triggered' (for example, '%s triggered') and the hash
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

Searches can be restarted or parallel searches can be managed
using the -R and -X flags.  -R 1yz assumes that yz is known to
fail, will start at 1yz, and if that does not fail, will try
0yz.  -X takes a list (space, comma, +, or - separated) of binary
suffixes to exclude from the restarted search.

The %s command can be run as its own test with the -F flag, as in
(prints about 100 long lines, and demonstrates multi-point failure detection):

  %s %s -F 
`,
			os.Args[0], hash_ev_name, os.Args[0], os.Args[0], os.Args[0])
	}

	flag.Parse()

	// Choose differently each time run to make it easier
	// to search for multiple failures; perhaps one is
	// substantially easier to debug in isolation.
	// TODO print this and also take it as a parameter; use it for the logfile name.
	rand.Seed(seed)

	if fma && loopvar {
		fmt.Printf("Cannot set both -fma and -loopvar")
		os.Exit(1)
	}

	if fma {
		hash_ev_string = "fmahash"
	}

	if loopvar {
		hash_ev_string = "loopvarhash"
	}

	hash_ev_name = hash_ev_string
	if i := strings.Index(hash_ev_name, "="); i != -1 {
		hash_ev_name = hash_ev_name[:i]
	}

	var ok error
	tmpdir, ok = ioutil.TempDir("", "gshstmp")
	if ok != nil {
		fmt.Printf("Failed to create temporary directory")
		os.Exit(1)
	}

	if function_selection_use_file {
		function_selection_use_stdout = false
		function_selection_logfile = filepath.Join(tmpdir, hash_ev_name+".triggered")
	}

	if fail {
		// Be a test program instead.
		test()
		return
	}

	envEnvPrefix = initialEnvEnvPrefix

	// For the Go compiler, splice in existing values of GOCOMPILEDEBUG
	if envEnvPrefix == "GOCOMPILEDEBUG=" {
		GCD := os.Getenv("GOCOMPILEDEBUG")
		if GCD != "" {
			envEnvPrefix = envEnvPrefix + GCD + ","
		}
	}

	excludes = parseExcludes(restartExclude)

	restArgs := flag.Args()
	var firstNotEnv int
	var arg string
	// pre-scan arguments for environment variable settings.
	for firstNotEnv, arg = range restArgs {
		if !strings.Contains(arg, "=") {
			break
		}
		if strings.HasPrefix(arg, initialEnvEnvPrefix) {
			if len(arg) == len(initialEnvEnvPrefix) {
				// if they did this, effect is to override the one in the environment,
				// so reset anything inherited from there.
				envEnvPrefix = initialEnvEnvPrefix
			} else {
				envEnvPrefix = envEnvPrefix + arg[len(initialEnvEnvPrefix):] + ","
			}
		} else {
			commandLineEnv = append(commandLineEnv, arg)
		}
	}
	args = append(args, restArgs[firstNotEnv:]...)

	// Extract test command and args if supplied.
	// note that initial arg has the default value to
	// make the -h output look right, so if there are
	// additional args, then it is discarded.
	args = args[1:]
	if len(args) > 0 {
		test_command = args[0]
		args = args[1:]
	}

	sss := []*searchState{}
	ss := &searchState{}
	if restartSuffix != "" {
		initialSuffix = restartSuffix[1:]
		restartSuffix = restartSuffix[:1]
	}
	for {
		if !ss.search(initialSuffix, restartSuffix) {
			fmt.Printf("FLAKY TEST OR BAD SEARCH\n")
			break
		} else {
			sss = append(sss, ss)
			// clean up multiple hash matches; this gives better output,
			// also makes excludes more precise when reporting multiple errors.
			ss.withoutExcludes = true
			ss.filter()

			multiple--
			if multiple == 0 {
				break
			}
			excludes = append(excludes, ss.suffix)
			if batchExclude {
				excludes = append(excludes, ss.hashes...)
			}
			ss = &searchState{}
			result, _ := ss.trySuffix(initialSuffix)
			if result == PASSED || result == PASSED0 {
				fmt.Printf("NO MORE FAILURES\n")
				break
			}
		}
	}

	excludes = nil

	for _, ss := range sss {
		ss.finish()
	}
}

func printCL() {
	for _, e := range commandLineEnv {
		fmt.Printf(" %s", e)
	}
	fmt.Printf(" %s", test_command)
	for _, e := range args {
		fmt.Printf(" %s", e)
	}
}

func (ss *searchState) filter() {
	if len(ss.hashes) > 0 {
		// Because the tests can be flaky, see if we accidentally included hashes that aren't
		// really necessary.  This is a boring mechanical task that computers excel at...

		fmt.Printf("Before filtering, multiple hashes required for failure:\n%s=%s", hash_ev_name, ss.suffix)
		for i, h := range ss.hashes {
			fmt.Printf(" %s%d=%s", hash_ev_name, i, h)
		}
		fmt.Println()

		// Next filter the hashes to see if any can be excluded:
		temporarily_removed := ss.hashes[len(ss.hashes)-1]
		ss.hashes = ss.hashes[0 : len(ss.hashes)-1]
		// suffix is initially the last value of GOSSAHASH
		var result int

		for i := len(ss.hashes); i >= -1 && len(ss.hashes) > 0; i-- {
			// Special values for search:
			// hashes[len(hashes)] == temporarily_removed,
			// hashes[-1] == suffix
			t := temporarily_removed
			if i == -1 {
				temporarily_removed = ss.suffix
				ss.suffix = t
			} else if i < len(ss.hashes) {
				temporarily_removed = ss.hashes[i]
				ss.hashes[i] = t
			}
			result, _ = ss.trySuffix(ss.suffix)
			switch result {
			case DONE0: // failed but GOSSAHASH triggered nothing
				// needed neither GOSSAHASH nor the excluded one.
				if len(ss.hashes) > 1 { // cannot be zero, see loop condition.
					temporarily_removed = ""
					ss.suffix = ss.hashes[len(ss.hashes)-1]
					ss.hashes = nil // exit with only suffix
				} else {
					ss.suffix = ss.hashes[len(ss.hashes)-1]
					temporarily_removed = ss.hashes[len(ss.hashes)-2]
					ss.hashes = ss.hashes[0 : len(ss.hashes)-2]
				}
			case DONE, FAILED: // ought not see failed, but never mind.
				temporarily_removed = ss.hashes[len(ss.hashes)-1]
				ss.hashes = ss.hashes[0 : len(ss.hashes)-1]
			}
		}
		if temporarily_removed != "" {
			ss.hashes = append(ss.hashes, temporarily_removed)
		}

		fmt.Printf("Confirming filtered hash set triggers failure:\n")
		_, ss.lastOutput = ss.trySuffix(ss.suffix)
	} else {
		fmt.Printf("Not filtering, single point failure\n")
	}
}

func (ss *searchState) finish() {
	printGSF := func() {
		if ss.lastTrigger != "" && !strings.HasPrefix(ss.lastTrigger, "POS=") {
			ci := strings.Index(ss.lastTrigger, ":")
			if ci == -1 {
				ci = len(ss.lastTrigger)
			}
			fmt.Printf("GOSSAFUNC='%s' ", ss.lastTrigger[:ci])
		}
	}

	printPOS := func(lastTrigger, intro string) {
		posPfx := "POS="
		if strings.HasPrefix(lastTrigger, posPfx) {
			inlineLocs := strings.Split(lastTrigger[len(posPfx):], ";")
			if len(inlineLocs) == 1 {
				fmt.Printf("%s %s\n", intro, inlineLocs[0])
			} else if len(inlineLocs) > 1 {
				fmt.Printf("%s:\n", intro)
				sfx := ""
				for _, l := range inlineLocs {
					fmt.Printf("\t%s%s\n", l, sfx)
					sfx = " (inlined function)"
				}
			}
		}

	}

	if len(ss.hashes) == 0 {
		fmt.Printf("FINISHED, suggest this command line for debugging:\n")
		printGSF()
		fmt.Printf("%s", ss.newStyleEnvString(false))
		printCL()
		fmt.Println()
		printPOS(ss.lastTrigger, "Problem is at")
	} else {
		fmt.Printf("FINISHED, after filtering, suggest this command line for debugging:\n")

		printGSF()
		fmt.Printf("%s", ss.newStyleEnvString(false))
		printCL()
		fmt.Println()

		output := ss.lastOutput
		_, trigger := matchTrigger(output, hash_ev_name, ss.suffix)
		printPOS(trigger, "Problem is at")
		for i, s := range ss.hashes {
			_, trigger = matchTrigger(output, fmt.Sprintf("%s%d", hash_ev_name, i), s)
			printPOS(trigger, "and")
		}
	}
}

func (ss *searchState) search(confirmed_suffix, restart_suffix string) bool {
	// confirmed_suffix is a suffix that is confirmed
	// to contain a failure.  The first confirmation is
	// assumed to have occurred externally before this
	// program was run.
	for len(confirmed_suffix) < hashLimit {
		a := "0"
		b := "1"

		if restart_suffix == "" && 0 == 8192&rand.Int() || restart_suffix == "1" {
			a, b = b, a
			restart_suffix = ""
		}
		first_result, _ := ss.trySuffix(a + confirmed_suffix)
		switch first_result {
		case FAILED:
			// Suffix is confirmed to contain a failure,
			// but there is more than one match (function compiled)
			// Record this confirmation and continue the search.
			confirmed_suffix = ss.suffix
			continue

		case PASSED0:
		case PASSED:
			// Suffix does not trigger a failure, so try
			// prepending a "1" instead, below.
		case DONE0:
			// Treat this like a "pass" -- this hashcode is not useful for failure.

		case DONE:
			// suffix caused exactly one function to be optimized
			// and the test also failed.
			if ss.next_singleton_hash_index == len(ss.hashes) {
				// In this case all confirmed searches have yielded
				// singleton instances and we are done.
				return true
			}
			// record this discovery and move on to the next one.
			confirmed_suffix = ss.hashes[ss.next_singleton_hash_index]
			ss.hashes[ss.next_singleton_hash_index] = ss.suffix
			ss.next_singleton_hash_index++
			continue
		}

		// The a arm contained no failures, try the b arm.
		result, _ := ss.trySuffix(b + confirmed_suffix)
		switch result {
		case FAILED:
			confirmed_suffix = ss.suffix
			continue
		case PASSED:
			if first_result == PASSED {
				fmt.Fprintf(os.Stdout, "Both trials unexpectedly succeeded\n")
				// 0xyz and 1xyz both succeeded alone, but xyz failed.
				// Failure therefore requires at least 2 hits, one in
				// 0xyz and one in 1xyz.  Therefore, put 1xyz in the set
				// of confirmed (i.e., contains a non-isolated failure)
				// mark 0xyz as confirmed for local search, and continue.
				if 0 == 8192&rand.Int() {
					a, b = b, a
				}
				ss.hashes = append(ss.hashes, b+confirmed_suffix)
				confirmed_suffix = a + confirmed_suffix
				continue
			}
			fallthrough

		case PASSED0, DONE0:
			// If we are here, the test is flaky.
			fmt.Fprintf(os.Stdout, "Combination of empty and pass, discard path (test is flaky)\n")
			if ss.next_singleton_hash_index == len(ss.hashes) {
				return false
			}
			confirmed_suffix = ss.hashes[len(ss.hashes)-1]
			ss.hashes = ss.hashes[0 : len(ss.hashes)-1]
			continue

		case DONE:
			if ss.next_singleton_hash_index == len(ss.hashes) {
				return true
			}
			// Randomly choose another place to work.
			j := rand.Intn(len(ss.hashes)-ss.next_singleton_hash_index) + ss.next_singleton_hash_index
			confirmed_suffix = ss.hashes[j]
			ss.hashes[j] = ss.hashes[ss.next_singleton_hash_index]
			ss.hashes[ss.next_singleton_hash_index] = ss.suffix
			ss.next_singleton_hash_index++
			continue
		}
	}
	return false
}

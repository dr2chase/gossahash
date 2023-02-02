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
	"strings"
	"time"
)

var (
	hashLimit       int    = 30 // Maximum length of a hash string
	test_command    string = "./gshs_test.bash"
	suffix          string = ""           // The initial hash suffix assumed to cause failure.
	logPrefix       string = "GSHS_LAST_" // Prefix on PASS/FAIL log files.
	verbose         bool   = false
	swapPassAndFail bool   = false
	old             bool   = false
	timeout         int    = 900 // Timeout in seconds to apply to command; failure if hit

	// Name of the environment variable that contains the hash suffix to be matched against function name hashes.
	hash_ev_name = "gossahash"
	// Expect to see this in the output when a value for gossahash triggers SSA-compilation of a function.
	function_selection_string     string
	function_selection_logfile    string
	function_selection_use_stdout bool = true  // Use stdout instead of a file (now default, old flag)
	function_selection_use_file   bool = false // Use file instead of stdout

	lastTrigger string

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

// The accumulated list of hashes that are either proven
// singleton triggers that contribute to failure, or proven/
// inferred to trigger at least one SSA-compilation that
// contributes to failure.
var hashes []string

// hashes before  this index correspond to a single function
// whose compilation is necessary to trigger a failure.
// This counter advances as new singleton-triggering hashes
// are found.
var next_singleton_hash_index int

const GCDprefix = "GOCOMPILEDEBUG="

var envEnvPrefix = GCDprefix

// hashPrefix is a string that precedes the hashcodes, for signalling
// different sorts of hashing (e.g., full path name vs basename)
var hashPrefix = ""

var sep = "/"

func newStyleEnvString() string {
	ev := fmt.Sprintf("%s%s=%s%s", envEnvPrefix, hash_ev_name, hashPrefix, suffix)
	for i := 0; i < len(hashes); i++ {
		ev += fmt.Sprintf("%s%s", sep, hashes[i])
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
func tryCmd(suffix string) (output []byte, err error) {
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

	if old {
		ev := fmt.Sprintf("%s=%s", hash_ev_name, suffix)
		extraEnv = append(extraEnv, ev)

		for i := 0; i < len(hashes); i++ {
			ev = fmt.Sprintf("%s%d=%s", hash_ev_name, i, hashes[i])
			extraEnv = append(extraEnv, ev)
		}
	} else {
		extraEnv = append(extraEnv, newStyleEnvString())
	}

	extraEnv = append(extraEnv, commandLineEnv...)

	cmd.Env = append(cmd.Env, extraEnv...)

	if verbose || true {
		fmt.Fprintf(os.Stdout, "Trying %s args=%s, env=%s\n", test_command, args, extraEnv)
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

// matchTrigger extracts hash trigger reports from the output.
// repeats are collapsed, but counted in the returned map.  The
// last match is also returned.
func matchTrigger(output []byte, hash_ev_name string) (map[string]int, string) {
	triggerPrefix := hash_ev_name + " triggered"
	m := make(map[string]int)
	var lastTrigger string
	scanner := bufio.NewScanner(bytes.NewBuffer(output))
	for scanner.Scan() {
		s := scanner.Text()
		if strings.Contains(s, triggerPrefix) {
			m[s] = m[s] + 1
			space := strings.LastIndex(s, " ")
			if space == -1 {
				space = len(s)
			}
			lastTrigger = strings.TrimSpace(s[len(triggerPrefix):space])
		}
	}
	return m, lastTrigger
}

// trySuffix runs the test command passing it suffix as an argument,
// and returns PASSED/FAILED/DONE/DONE0 based on return code and occurrences
// of the function_selection_string within the output; if there is only
// one and the command fails, then the search is done.
// Appropriate log files and narrative are also produced.
func trySuffix(suffix string) (int, []byte) {
	output, error := tryCmd(suffix)

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
	m, lastTrigger = matchTrigger(output, hash_ev_name)
	count := len(m)

	// (error == nil) means success
	prefix := ""
	if swapPassAndFail {
		prefix = "NOT-"
	}
	if (error == nil) == swapPassAndFail {
		why := "success treated as failure"
		if error != nil {
			why = error.Error()
		}
		// we like errors.
		fmt.Fprintf(os.Stdout, "%s %sfailed (%d distinct triggers): %s\n", test_command, prefix, count, why)
		lfn := fmt.Sprintf("%s%sFAIL.%d.log", logPrefix, prefix, next_singleton_hash_index)
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
	hash_option_info := hash_ev_name + "/(if -O)" + strings.ToUpper(hash_ev_name)
	hash_option_name := ""
	fma := false
	flag.IntVar(&timeout, "t", timeout, "timeout in seconds for running test script, 0=run till done. Negative timeout means timing out is a pass, not a failure")

	// flag.Var(&args, "c", "executable file to run.\n"+
	// 	"\tMay be repeated to supply leading args to command.\n\t") // default on next line

	flag.StringVar(&hash_option_name, "e", hash_option_info, "name/prefix of environment variable communicating hash suffix")

	flag.BoolVar(&swapPassAndFail, "X", swapPassAndFail, "swap pass and fail for test script (default false)")
	flag.BoolVar(&verbose, "v", verbose, "also print output of test script (default false)")
	flag.BoolVar(&old, "O", old, "use old environment variable protocol")

	flag.IntVar(&hashLimit, "n", hashLimit, "maximum hash suffix length to try before giving up")

	flag.StringVar(&logPrefix, "l", logPrefix, "prefix of log file names ending ...{PASS,FAIL}.log")

	flag.StringVar(&suffix, "P", suffix, "root of hash suffix to begin searching at (default empty)")
	flag.StringVar(&hashPrefix, "H", hashPrefix, "string prepended to all hash encodings, for special hash interpretation/debugging")

	flag.BoolVar(&function_selection_use_stdout, "s", function_selection_use_stdout, "use stdout for 'triggered' communication (obsolete, now default)")
	flag.BoolVar(&function_selection_use_file, "f", function_selection_use_file, "use file for 'triggered' communication (sets GSHS_LOGFILE)")

	flag.BoolVar(&fail, "F", fail, "act as a test program")
	flag.BoolVar(&fma, "fma", fma, "search for fma problems")

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

The %s command can be run as its own test with the -F flag, as in
(prints about 100 long lines, and demonstrates multi-point failure detection):

  %s %s -F 

This Go code can be used to trigger the tested behavior:

func doit(name string) bool {
    if os.Getenv("GOSSAHASH") == "" {
        return true  // Default behavior is yes.
    }
    // Check hash of name against a partial input hash.  We use this feature 
    // to do a binary search to find a function that is incorrectly compiled.
    hstr := ""
    for _, b := range sha1.Sum([]byte(name)) {
        hstr += fmt.Sprintf("%%08b", b)
    }
    if strings.HasSuffix(hstr, os.Getenv("GOSSAHASH")) {
        fmt.Printf("GOSSAHASH triggered %%s\n", name)
        return true
    }
    // Iteratively try additional hashes to allow tests for multi-point failure.
    for i := 0; true; i++ {
        ev := fmt.Sprintf("GOSSAHASH%%d", i)
        evv := os.Getenv(ev)
        if evv == "" {
            break
        }
        if strings.HasSuffix(hstr, evv) {
            fmt.Printf("%%s triggered %%s\n", ev, name)
            return true
        }
    }
    return false
}
`,
			os.Args[0], hash_ev_name, os.Args[0], os.Args[0], os.Args[0])
	}

	flag.Parse()

	// Choose differently each time run to make it easier
	// to search for multiple failures; perhaps one is
	// substantially easier to debug in isolation.
	// TODO print this and also take it as a parameter; use it for the logfile name.
	seed := time.Now().UnixNano()
	rand.Seed(seed)

	if hash_option_name == hash_option_info {
		if old {
			if fma {
				fmt.Fprintf(os.Stderr, "-fma and -O are incompatible (-fma changes the environment variable)\n")
				os.Exit(1)
			}
			hash_ev_name = strings.ToUpper(hash_ev_name)
		} else if fma {
			hash_ev_name = "fmahash"
		}
	} else {
		if fma {
			fmt.Fprintf(os.Stderr, "-fma and -e are incompatible (-fma changes the environment variable)\n")
			os.Exit(1)
		}
		hash_ev_name = hash_option_name
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

	GCD := os.Getenv("GOCOMPILEDEBUG")
	if GCD != "" {
		envEnvPrefix = envEnvPrefix + GCD + ","
	}

	restArgs := flag.Args()
	var firstNotEnv int
	var arg string
	for firstNotEnv, arg = range restArgs {
		if !strings.Contains(arg, "=") {
			break
		}
		if !old && strings.HasPrefix(arg, GCDprefix) {
			if len(arg) == len(GCDprefix) {
				// if they did this, effect is to override the one in the environment,
				// so reset anything inherited from there.
				envEnvPrefix = GCDprefix
			} else {
				envEnvPrefix = envEnvPrefix + arg[len(GCDprefix):] + ","
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

	// confirmed_suffix is a suffix that is confirmed
	// to contain a failure.  The first confirmation is
	// assumed to have occurred externally before this
	// program was run.
	confirmed_suffix := suffix
searchloop:
	for len(confirmed_suffix) < hashLimit {
		a := "0"
		b := "1"
		if 0 == 8192&rand.Int() {
			t := a
			a = b
			b = t
		}
		suffix = a + confirmed_suffix
		first_result, _ := trySuffix(suffix)
		switch first_result {
		case FAILED:
			// Suffix is confirmed to contain a failure,
			// but there is more than one match (function compiled)
			// Record this confirmation and continue the search.
			confirmed_suffix = suffix
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
			if next_singleton_hash_index == len(hashes) {
				// In this case all confirmed searches have yielded
				// singleton instances and we are done.
				break searchloop
			}
			// record this discovery and move on to the next one.
			confirmed_suffix = hashes[next_singleton_hash_index]
			hashes[next_singleton_hash_index] = suffix
			next_singleton_hash_index++
			continue
		}

		// The a arm contained no failures, try the b arm.
		suffix = b + confirmed_suffix
		result, _ := trySuffix(suffix)
		switch result {
		case FAILED:
			confirmed_suffix = suffix
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
					t := a
					a = b
					b = t
				}
				hashes = append(hashes, b+confirmed_suffix)
				confirmed_suffix = a + confirmed_suffix
				continue
			}
			fallthrough

		case PASSED0, DONE0:
			// If we are here, the test is flaky.
			fmt.Fprintf(os.Stdout, "Combination of empty and pass, discard path (test is flaky)\n")
			if next_singleton_hash_index == len(hashes) {
				break searchloop
			}
			confirmed_suffix = hashes[len(hashes)-1]
			hashes = hashes[0 : len(hashes)-1]
			continue

		case DONE:
			if next_singleton_hash_index == len(hashes) {
				break searchloop
			}
			// Randomly choose another place to work.
			j := rand.Intn(len(hashes) - next_singleton_hash_index)
			confirmed_suffix = hashes[next_singleton_hash_index+j]
			hashes[next_singleton_hash_index+j] = hashes[next_singleton_hash_index]
			hashes[next_singleton_hash_index] = suffix
			next_singleton_hash_index++
			continue
		}
	}

	printCL := func() {
		for _, e := range commandLineEnv {
			fmt.Printf(" %s", e)
		}
		fmt.Printf(" %s", test_command)
		for _, e := range args {
			fmt.Printf(" %s", e)
		}
	}

	printGSF := func() {
		if lastTrigger != "" && !strings.HasPrefix(lastTrigger, "POS=") {
			ci := strings.Index(lastTrigger, ":")
			if ci == -1 {
				ci = len(lastTrigger)
			}
			fmt.Printf("GOSSAFUNC='%s' ", lastTrigger[:ci])
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

	if len(hashes) == 0 {
		fmt.Printf("FINISHED, suggest this command line for debugging:\n")
		printGSF()
		if old {
			fmt.Printf("%s=%s", hash_ev_name, string(suffix))
		} else {
			fmt.Printf("%s", newStyleEnvString())
		}
		printCL()
		fmt.Println()
		printPOS(lastTrigger, "Problem is at")
	} else {
		// Because the tests can be flaky, see if we accidentally included hashes that aren't
		// really necessary.  This is a boring mechanical task that computers excel at...

		fmt.Printf("Before filtering, multiple hashes required for failure:\n%s=%s", hash_ev_name, suffix)
		for i := 0; i < len(hashes); i++ {
			fmt.Printf(" %s%d=%s", hash_ev_name, i, hashes[i])
		}
		fmt.Println()

		// Next filter the hashes to see if any can be excluded:
		temporarily_removed := hashes[len(hashes)-1]
		hashes = hashes[0 : len(hashes)-1]
		// suffix is initially the last value of GOSSAHASH
		var result int
		var output []byte

		for i := len(hashes); i >= -1 && len(hashes) > 0; i-- {
			// Special values for search:
			// hashes[len(hashes)] == temporarily_removed,
			// hashes[-1] == suffix
			t := temporarily_removed
			if i == -1 {
				temporarily_removed = suffix
				suffix = t
			} else if i < len(hashes) {
				temporarily_removed = hashes[i]
				hashes[i] = t
			}
			result, output = trySuffix(suffix)
			switch result {
			case DONE0: // failed but GOSSAHASH triggered nothing
				// needed neither GOSSAHASH nor the excluded one.
				if len(hashes) > 1 { // cannot be zero, see loop condition.
					temporarily_removed = ""
					suffix = hashes[len(hashes)-1]
					hashes = nil // exit with only suffix
				} else {
					suffix = hashes[len(hashes)-1]
					temporarily_removed = hashes[len(hashes)-2]
					hashes = hashes[0 : len(hashes)-2]
				}
			case DONE, FAILED: // ought not see failed, but never mind.
				temporarily_removed = hashes[len(hashes)-1]
				hashes = hashes[0 : len(hashes)-1]
			}
		}
		if temporarily_removed != "" {
			hashes = append(hashes, temporarily_removed)
		}

		fmt.Printf("Confirming filtered hash set triggers failure:\n")
		result, output = trySuffix(suffix)

		fmt.Printf("FINISHED, after filtering, suggest this command line for debugging:\n")

		printGSF()
		if old {
			fmt.Printf("%s=%s", hash_ev_name, suffix)

			for i := 0; i < len(hashes); i++ {
				fmt.Printf(" %s%d=%s", hash_ev_name, i, hashes[i])
			}
		} else {
			fmt.Printf("%s", newStyleEnvString())
		}
		printCL()
		fmt.Println()

		_, trigger := matchTrigger(output, hash_ev_name)
		printPOS(trigger, "Problem is at")
		for i := 0; i < len(hashes); i++ {
			_, trigger = matchTrigger(output, fmt.Sprintf("%s%d", hash_ev_name, i))
			printPOS(trigger, "and")
		}
	}
}

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"time"
)

var (
	hashLimit    int    = 30 // Maximum length of a hash string
	test_command string = "./gshs_test.bash"
	suffix       string = ""           // The initial hash suffix assumed to cause failure.
	logPrefix    string = "GSHS_LAST_" // Prefix on PASS/FAIL log files.
	verbose      bool   = false
	timeout      int    = 30 // Timeout to apply to command; failure if hit

	// Name of the environment variable that contains the hash suffix to be matched against function name hashes.
	hash_ev_name = "GOSSAHASH"
	// Expect to see this in the output when a value for GOSSAHASH triggers SSA-compilation of a function.
	function_selection_string = hash_ev_name + " triggered"

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

var args arg = arg{test_command}

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
	extraenv := make([]string, 0)
	ev := fmt.Sprintf("%s=%s", hash_ev_name, suffix)
	cmd.Env = append(cmd.Env, ev)
	extraenv = append(extraenv, ev)
	for i := 0; i < len(hashes); i++ {
		ev = fmt.Sprintf("%s%d=%s", hash_ev_name, i, hashes[i])
		cmd.Env = append(cmd.Env, ev)
		extraenv = append(extraenv, ev)
	}

	if verbose {
		fmt.Fprintf(os.Stdout, "Trying %s args=%s, env=%s\n", test_command, cmd.Args, extraenv)
	} else {
		if len(extraenv) == 0 {
			fmt.Fprintf(os.Stdout, "Trying %s\n", suffix)
		} else {
			fmt.Fprintf(os.Stdout, "Trying %s\n", extraenv)
		}
	}

	if timeout <= 0 {
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
		timer := time.AfterFunc(time.Second*time.Duration(timeout), func() {
			killErr = cmd.Process.Signal(os.Kill)
		})
		err = cmd.Wait()
		if killErr != nil {
			// Not sure what I would do with this,
			// and it could appear merely as the result of a lost race.
		}
		timer.Stop()
		output = b.Bytes()
	}

	if verbose {
		fmt.Fprintf(os.Stdout, "%s", string(output))
	}
	return
}

// trySuffix runs the test command passing it suffix as an argument,
// and returns PASSED/FAILED/DONE/DONE0 based on return code and occurrences
// of the function_selection_string within the output; if there is only
// one and the command fails, then the search is done.
// Appropriate log files and narrative are also produced.
func trySuffix(suffix string) int {
	function_selection_bytes := []byte(function_selection_string)
	output, error := tryCmd(suffix)
	count := bytes.Count(output, function_selection_bytes)

	if error != nil {
		// we like errors.
		fmt.Fprintf(os.Stdout, "%s failed: %s\n", test_command, error.Error())
		lfn := fmt.Sprintf("%sFAIL.%d.log", logPrefix, next_singleton_hash_index)
		saveLogFile(lfn, output)
		if count <= 1 {
			fmt.Fprintf(os.Stdout, "Review %s for failing run\n", lfn)
			if count == 0 {
				return DONE0
			}
			return DONE
		}
		return FAILED
	}
	saveLogFile(logPrefix+"PASS.log", output)
	if count == 0 {
		return PASSED0
	}
	return PASSED
}

func main() {
	flag.Var(&args, "c", "executable file of one arg hashstring to run.\n"+
		"\tMay be repeated to supply leading args to command.\n\t") // default on next line

	flag.StringVar(&logPrefix, "l", logPrefix, "prefix of log file names ending ...{PASS,FAIL}.log")
	flag.IntVar(&hashLimit, "n", hashLimit, "maximum hash string length to try before giving up")
	flag.StringVar(&suffix, "P", suffix, "root string to begin searching at (default empty)")
	flag.BoolVar(&verbose, "v", verbose, "also print output of test script (default false)")
	flag.IntVar(&timeout, "t", timeout, "timeout in seconds for running test script, 0=run till done")
	flag.BoolVar(&fail, "f", fail, "act as a test program")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr,
			`
%s runs
`,
			os.Args[0])
	}

	flag.Parse()

	if fail {
		// Be a test program instead.
		test()
		return
	}

	// Extract test command and args if supplied.
	if len(args) > 0 {
		test_command = args[0]
		args = args[1:]
	}

	rand.Seed(time.Now().UnixNano())

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
		first_result := trySuffix(suffix)
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
		switch trySuffix(suffix) {
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

	fmt.Fprintf(os.Stdout, "Finished with GOSSAHASH=%s\n", string(suffix))

	// Because the tests can be flaky, see if we accidentally included hashes that aren't
	// really necessary.  This is a boring mechanical task that computers excel at...
	if len(hashes) > 0 {
		fmt.Printf("Before filtering, multiple methods required for failure:\nGOSSAHASH=%s", suffix)
		for i := 0; i < len(hashes); i++ {
			fmt.Printf(" %s%d=%s", hash_ev_name, i, hashes[i])
		}
		fmt.Println()

		// Next filter the hashes to see if any can be excluded:
		temporarily_removed := hashes[len(hashes)-1]
		hashes = hashes[0 : len(hashes)-1]
		// suffix is initially the last value of GOSSAHASH

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
			switch trySuffix(suffix) {
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
		fmt.Printf("After filtering, methods required for failure:\nGOSSAHASH=%s", suffix)
		for i := 0; i < len(hashes); i++ {
			fmt.Printf(" %s%d=%s", hash_ev_name, i, hashes[i])
		}
		fmt.Println()

	}
}

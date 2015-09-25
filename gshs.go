package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"time"
)

var (
	hashLimit                 int    = 30
	test_command              string = "./gshs_test.bash"
	suffix                    string = "" // If not empty default, fix the flag default below.
	logPrefix                 string = "GSHS_LAST_"
	verbose                   bool   = false
	timeout                   int    = 30 // Timeout to apply to command; failure if hit
	hash_ev_name                     = "GOSSAHASH"
	function_selection_string        = hash_ev_name + " triggered"
	fail                      bool
)

const (
	FAILED = iota
	PASSED
	PASSED0 // no hits, thus passed.
	DONE
	DONE0 // not supposed to happen; failure without adding anything new.
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

var hashes []string

// hashes below this correspond to a single function whose
// compilation is necessary to trigger a failure.
var next_singleton_hash_index int

// hashes below this are confirmed to contain a function
// whose compilation is necessary to trigger a failure.
// failure is either directly verified or inferred.
// var next_confirmed_hash_index int

// tryCmd runs the test command with suffix appended to the args.
// If timeout is greater than zero then the command will be
// killed after that many seconds (to help with bugs that exhibit
// as an infinite loop), otherwise it runs to completion and the
// error code and output are captureed and returned.
func tryCmd(suffix string) (output []byte, err error) {
	cmd := exec.Command(test_command)
	cmd.Args = append(cmd.Args, args...)
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
		fmt.Fprintf(os.Stdout, "Trying %s, %v + %s\n", test_command, cmd.Args, extraenv)
	} else {
		if len(extraenv) == 0 {
			fmt.Fprintf(os.Stdout, "Trying %s\n", suffix)
		} else {
			fmt.Fprintf(os.Stdout, "Trying %s + %s\n", suffix, extraenv)
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
		saveLogFile(logPrefix+"FAIL.log", output)
		if count <= 1 {
			fmt.Fprintf(os.Stdout, "Review %s for failing run\n", logPrefix+"FAIL.log")
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
		test()
		return
	}

	// Extract test command and args if supplied.
	if len(args) > 0 {
		test_command = args[0]
		args = args[1:]
	}

	// confirmed_suffix is a suffix that is confirmed (initially asserted)
	// to contain a failure.
	confirmed_suffix := suffix
loop:
	for len(confirmed_suffix) < hashLimit {
		suffix = "0" + confirmed_suffix
		first_result := trySuffix(suffix)
		switch first_result {
		case FAILED:
			// Suffix is confirmed to contain a failure,
			// but there is more than one match (function compiled)
			confirmed_suffix = suffix
			continue

		case PASSED0:
		case PASSED:
			// Suffix does not trigger a failure

		case DONE0:
			// Treat this like a "pass" -- this hashcode is not useful for failure.

		case DONE:
			// suffix caused exactly one function to be optimized
			// and the test also failed.
			if next_singleton_hash_index == len(hashes) {
				break loop
			}
			confirmed_suffix = hashes[next_singleton_hash_index]
			hashes[next_singleton_hash_index] = suffix
			next_singleton_hash_index++
			continue
		}
		suffix = "1" + confirmed_suffix
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
				hashes = append(hashes, suffix)
				confirmed_suffix = "0" + confirmed_suffix
				continue
			}
			fallthrough

		case PASSED0, DONE0:
			fmt.Fprintf(os.Stdout, "Combination of empty and pass, discard path\n")
			if next_singleton_hash_index == len(hashes) {
				break loop
			}
			confirmed_suffix = hashes[len(hashes)-1]
			hashes = hashes[0 : len(hashes)-1]
			continue

		case DONE:
			if next_singleton_hash_index == len(hashes) {
				break loop
			}
			confirmed_suffix = hashes[next_singleton_hash_index]
			hashes[next_singleton_hash_index] = suffix
			next_singleton_hash_index++
			continue
		}
	}

	fmt.Fprintf(os.Stdout, "Finished with GOSSAHASH=%s\n", string(suffix))
	if len(hashes) > 0 {
		fmt.Printf("Before filtering, multiple methods required for failure:\nGOSSAHASH=%s", suffix)
		for i := 0; i < len(hashes); i++ {
			fmt.Printf(" %s%d=%s", hash_ev_name, i, hashes[i])
		}
		fmt.Println()
		// Next filter the hashes to see if any can be excluded:
		next_confirmed_hash_index := len(hashes) - 1
		temporarily_removed := hashes[next_confirmed_hash_index]
		hashes = hashes[0:next_confirmed_hash_index]
		for i := next_confirmed_hash_index; i >= -1 && next_confirmed_hash_index >= 0; i-- {
			// do a run excluding hashes[i], where hashes[-1] == suffix
			t := temporarily_removed
			if i == -1 {
				temporarily_removed = suffix
				suffix = t
			} else if i < len(hashes) {
				temporarily_removed = hashes[i]
				hashes[i] = t
			} // temporarily_removed is the excluded one if i == len(hashes)
			switch trySuffix(suffix) {
			case DONE0: // failed but GOSSAHASH triggered nothing
				// needed neither GOSSAHASH nor the excluded one.
				next_confirmed_hash_index--
				suffix = hashes[next_confirmed_hash_index]
				next_confirmed_hash_index--
				hashes = hashes[0:next_confirmed_hash_index]

			case DONE, FAILED: // ought not see failed....
				next_confirmed_hash_index--
				hashes = hashes[0:next_confirmed_hash_index]
			}
		}
		hashes = append(hashes, temporarily_removed)
		fmt.Printf("After filtering, methods required for failure:\nGOSSAHASH=%s", suffix)
		for i := 0; i < len(hashes); i++ {
			fmt.Printf(" %s%d=%s", hash_ev_name, i, hashes[i])
		}
		fmt.Println()

	}
}

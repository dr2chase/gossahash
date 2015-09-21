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
	hashLimit                 int    = 20
	test_command              string = "./gshs_test.bash"
	suffix                    string = "" // If not empty default, fix the flag default below.
	logPrefix                 string = "GSHS_LAST_"
	verbose                   bool   = false
	timeout                   int    = 10 // Timeout to apply to command; failure if hit
	function_selection_string        = "GOSSAHASH triggered"
)

const (
	FAILED = iota
	PASSED
	DONE
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

// tryCmd runs the test command with suffix appended to the args.
// If timeout is greater than zero then the command will be
// killed after that many seconds (to help with bugs that exhibit
// as an infinite loop), otherwise it runs to completion and the
// error code and output are captureed and returned.
func tryCmd(suffix string) (output []byte, err error) {
	cmd := exec.Command(test_command)
	cmd.Args = append(cmd.Args, args...)
	cmd.Args = append(cmd.Args, suffix)
	if verbose {
		fmt.Fprintf(os.Stdout, "Trying %s,%v\n", test_command, cmd.Args)
	} else {
		fmt.Fprintf(os.Stdout, "Trying %s\n", suffix)
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
// and returns PASSED/FAILED/DONE based on return code and occurrences
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
		if count == 1 {
			fmt.Fprintf(os.Stdout, "Review %s for failing run\n", logPrefix+"FAIL.log")
			return DONE
		}
		return FAILED
	} else {
		saveLogFile(logPrefix+"PASS.log", output)
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

	// function_selection_token := []byte("GOSSAHASH triggered")

	// Extract test command and args if supplied.
	if len(args) > 0 {
		test_command = args[0]
		args = args[1:]
	}

	confirmed_suffix := suffix

loop:
	for len(confirmed_suffix) < hashLimit {

		suffix = "0" + confirmed_suffix

		switch trySuffix(suffix) {
		case FAILED:
			confirmed_suffix = suffix
			continue
		case PASSED:
		case DONE:
			break loop
		}

		suffix = "1" + confirmed_suffix

		switch trySuffix(suffix) {
		case FAILED:
			confirmed_suffix = suffix
			continue
		case PASSED:
			fmt.Fprintf(os.Stdout, "Both trials unexpectedly succeeded", string(suffix))
			break loop
		case DONE:
			break loop
		}

		// TODO: if 0xyz and 1xyz both succeed but xyz failed,
		// that means the failure requires two or more functions
		// to be SSA-compiled.  With a multiple hash matcher
		// (e.g., that tries GOSSAHASH1, GOSSAHASH2, etc)
		// the search can proceed by continuing the search
		// at GOSSAHASH1=1xyz GOSSAHASH={0,1}0xyz.
		// Once GOSSAHASH converges on the single point,
		// the roles can be reversed and the search continued
		// at GOSSAHASH={0,1}1xyz GOSSAHASH1=abc0xyz

	}
	fmt.Fprintf(os.Stdout, "Finished with suffix = %s\n", string(suffix))
}

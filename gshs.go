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
	hashLimit    int    = 20
	test_command string = "./gshs_test.bash"
	suffix       string = "" // If not empty default, fix the flag default below.
	logPrefix    string = "GSHS_LAST_"
	verbose      bool   = false
	timeout      int    = 30 // Timeout to apply to command; failure if hit
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

var args arg

func (a *arg) String() string {
	return fmt.Sprint("%v", *a)
}

func (a *arg) Set(value string) error {
	*a = append([]string(*a), value)
	return nil
}

// try runs the test command with suffix appended to the args.
// If timeout is greater than zero then the command will be
// killed after that many seconds (to help with bugs that exhibit
// as an infinite loop), otherwise it runs to completion and the
// error code and output are captureed and returned.
func try(suffix string) (output []byte, err error) {
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

func main() {
	flag.Var(&args, "c", "executable file of one arg hashstring to run.\n\tMay be repeated to supply leading args to command.\n\t")
	flag.StringVar(&logPrefix, "l", logPrefix, "prefix of log file names ending ...{PASS,FAIL}.log\n\t")
	flag.IntVar(&hashLimit, "n", hashLimit, "maximum string length to search before giving up\n\t")
	flag.StringVar(&suffix, "P", suffix, "root string to begin searching at (default is empty)")
	flag.BoolVar(&verbose, "v", verbose, "also print output of test script (default is false)")
	flag.IntVar(&timeout, "t", timeout, "timeout in seconds for running test script, default is 0 (run till done)")

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

	function_selection_token := []byte("GOSSAHASH triggered")

	// Extract test command and args if supplied.
	if len(args) > 0 {
		test_command = args[0]
		args = args[1:]
	}

	confirmed_suffix := suffix

	for len(confirmed_suffix) < hashLimit {

		suffix = "0" + confirmed_suffix
		output, error := try(suffix)
		count := bytes.Count(output, function_selection_token)

		if error != nil {
			// we like errors.
			confirmed_suffix = suffix
			fmt.Fprintf(os.Stdout, "%s failed: %s\n", test_command, error.Error())
			saveLogFile(logPrefix+"FAIL.log", output)
			if count == 1 {
				fmt.Fprintf(os.Stdout, "Review %s for failing run\n", logPrefix+"FAIL.log")
				break
			}
			continue
		} else {
			saveLogFile(logPrefix+"PASS.log", output)
		}

		suffix = "1" + confirmed_suffix
		output, error = try(suffix)
		count = bytes.Count(output, function_selection_token)

		if error != nil {
			// we like errors.
			confirmed_suffix = suffix
			fmt.Fprintf(os.Stdout, "%s failed: %s\n", test_command, error.Error())
			saveLogFile(logPrefix+"FAIL.log", output)
			if count == 1 {
				fmt.Fprintf(os.Stdout, "Review %s for failing run\n", logPrefix+"FAIL.log")
				break
			}
			continue
		} else {
			saveLogFile(logPrefix+"PASS.log", output)
			fmt.Fprintf(os.Stdout, "Both trials unexpectedly succeeded", string(suffix))
			break
		}
	}
	fmt.Fprintf(os.Stdout, "Finished with suffix = %s\n", string(suffix))
}

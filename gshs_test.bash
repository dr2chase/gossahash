#!/bin/bash
# Expect to see GOSSAHASH in environment
# Typically
# GOSSAPKG=flate go test -v -run TestForwardCopy .
# But for testing do this -- requires 8 hashes to fail
./gossahash-search -f

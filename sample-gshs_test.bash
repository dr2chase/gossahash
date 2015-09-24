#!/bin/bash
# Expect to see GOSSAHASH in environment
GOSSAPKG=flate go test -v -run TestForwardCopy .

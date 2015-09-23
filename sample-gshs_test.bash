#!/bin/bash
GOSSAPKG=flate GOSSAHASH="$1" go test -v -run TestForwardCopy .

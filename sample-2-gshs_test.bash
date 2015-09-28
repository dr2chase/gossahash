#!/bin/bash
rm -f runtime.test
GOSSAPKG=runtime go test -c .

# Try to normalize the environment between runs.
unset GOSSAHASH
unset $( env | grep ^GOSSAHASH | sed 's/=.*//')

x="x"
# It must not pass more than twice out of seven to "fail"
if ./runtime.test -test.v -test.run '^TestChan$' ; then
	x=a$x
fi
if ./runtime.test -test.v -test.run '^TestChan$' ; then
	x=a$x
fi
if ./runtime.test -test.v -test.run '^TestChan$' ; then
	x=a$x
fi
if ./runtime.test -test.v -test.run '^TestChan$' ; then
	x=a$x
fi
if ./runtime.test -test.v -test.run '^TestChan$' ; then
	x=a$x
fi
if ./runtime.test -test.v -test.run '^TestChan$' ; then
	x=a$x
fi
if ./runtime.test -test.v -test.run '^TestChan$' ; then
	x=a$x
fi

if [ "$x" = "x" ] ; then
	exit 1
fi
if [ "$x" = "ax" ] ; then
	exit 1
fi

if [ "$x" = "aax" ] ; then
	exit 1
fi

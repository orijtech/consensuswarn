#!/bin/sh

curl -sL -H "Accept: application/vnd.github.v3.diff" "$1" | CGO_ENABLED=0 /statediff

retVal=$?

# Exit code 128 means the patch touches state code.
if [ $retVal -eq 128 ]; then
	echo "::warning ::PR possibly affects state"
fi

exit $retVal

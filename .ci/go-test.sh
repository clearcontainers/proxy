#!/bin/bash -e

script_dir=$(cd `dirname $0`; pwd)
root_dir=`dirname $script_dir`

test_packages="."
go_test_flags="-v -race -timeout 2s"

echo Running go test on packages "'$test_packages'" with flags "'$go_test_flags'"
function test_travis
{
	echo "mode: count" > profile.cov

	for pkg in $test_packages; do
		go test $go_test_flags -covermode=atomic -coverprofile=profile_tmp.cov $pkg
		[ -f profile_tmp.cov ] && tail -n +2 profile_tmp.cov >> profile.cov;
		rm -f profile_tmp.cov
	done
}

function test_local
{
	go test $go_test_flags $test_packages
}

if [ "$CI" = "true" ]; then
	test_travis
else
	test_local
fi

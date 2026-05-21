# Host-only unit tests for kmod/awg-proxy/src/.
#
# Build/run on a regular dev machine — NO Linux kernel headers required.
# We provide minimal stubs (in stubs/) for the few <linux/*> and <asm/*>
# includes that the source files pull in, and shim.h supplies the integer
# types, byte-order macros, and deterministic get_random_bytes / ktime_*.
#
# Targets:
#   make           run all tests (default)
#   make test      run all tests
#   make clean     remove built artifacts
#
# To keep determinism, every test seeds the PRNG via shim_set_random_seed()
# and pins the clock via shim_set_fixed_time(). Byte-exact assertions are
# reproducible across machines / compiler revisions.

CC      ?= gcc
CFLAGS  := -O0 -g3 -std=c11 -Wall -Wextra -Werror \
           -Wno-pointer-sign -Wno-unused-parameter \
           -I . -I stubs -include shim.h
LDFLAGS :=

SRC     := ../src

.PHONY: all test clean

all: test

test: test_cps test_transform test_tunnel
	./test_cps
	./test_transform
	./test_tunnel

test_cps: test_cps.c shim.c shim.h $(SRC)/cps.c
	$(CC) $(CFLAGS) -o $@ test_cps.c shim.c $(SRC)/cps.c $(LDFLAGS)

test_transform: test_transform.c shim.c shim.h $(SRC)/transform.c $(SRC)/blake2s.c $(SRC)/cps.c
	$(CC) $(CFLAGS) -o $@ test_transform.c shim.c $(SRC)/transform.c $(SRC)/blake2s.c $(SRC)/cps.c $(LDFLAGS)

test_tunnel: test_tunnel.c shim.c shim.h $(SRC)/tunnel.c $(SRC)/transform.c $(SRC)/blake2s.c $(SRC)/cps.c
	$(CC) $(CFLAGS) -o $@ test_tunnel.c shim.c $(SRC)/tunnel.c $(SRC)/transform.c $(SRC)/blake2s.c $(SRC)/cps.c $(LDFLAGS)

clean:
	rm -f test_cps test_transform test_tunnel *.o

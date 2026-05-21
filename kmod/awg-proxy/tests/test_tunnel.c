// SPDX-License-Identifier: GPL-2.0
/*
 * Userspace unit tests for kmod/awg-proxy/src/tunnel.c.
 */

#include "shim.h"
#include "../src/tunnel.h"

#include <string.h>
#include <stdio.h>
#include <stdarg.h>

static int tests_run, tests_failed;

static void test_fail(const char *test, const char *fmt, ...)
{
	va_list ap;

	fprintf(stderr, "FAIL %s: ", test);
	va_start(ap, fmt);
	vfprintf(stderr, fmt, ap);
	va_end(ap);
	fputc('\n', stderr);
	tests_failed++;
}

#define ASSERT_TRUE(test, cond, msg) do { \
	if (!(cond)) test_fail((test), "%s", (msg)); \
} while (0)

static void test_accepts_non_overlapping_h_ranges(void)
{
	awg_config_t cfg;
	int ret;

	tests_run++;
	ret = awg_config_parse("203.0.113.1:51820 H1=100-199 H2=200-299 H3=300-399 H4=400-499",
			       &cfg);
	ASSERT_TRUE("accepts_non_overlapping_h_ranges", ret == 0,
		    "non-overlapping H ranges should parse");
	if (!ret)
		awg_config_free(&cfg);
}

static void test_rejects_overlapping_h_ranges(void)
{
	awg_config_t cfg;
	int ret;

	tests_run++;
	ret = awg_config_parse("203.0.113.1:51820 H1=100-200 H2=200-300 H3=400 H4=500",
			       &cfg);
	ASSERT_TRUE("rejects_overlapping_h_ranges", ret != 0,
		    "H ranges sharing a boundary must be rejected");
}

static void test_rejects_range_overlapping_default_header(void)
{
	awg_config_t cfg;
	int ret;

	tests_run++;
	ret = awg_config_parse("203.0.113.1:51820 H2=1-20",
			       &cfg);
	ASSERT_TRUE("rejects_range_overlapping_default_header", ret != 0,
		    "configured H2 range must not overlap default H1=1");
}

int main(void)
{
	test_accepts_non_overlapping_h_ranges();
	test_rejects_overlapping_h_ranges();
	test_rejects_range_overlapping_default_header();

	printf("\n=== %d run, %d failed ===\n", tests_run, tests_failed);
	return tests_failed == 0 ? 0 : 1;
}

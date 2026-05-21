/* Host-build stub for <linux/inet.h>. */
#ifndef _STUB_LINUX_INET_H
#define _STUB_LINUX_INET_H

#include <arpa/inet.h>

static inline __be32 in_aton(const char *str)
{
	return (__be32)inet_addr(str);
}

#endif

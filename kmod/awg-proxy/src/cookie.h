/* SPDX-License-Identifier: GPL-2.0 */
#ifndef _AWG_PROXY_COOKIE_H
#define _AWG_PROXY_COOKIE_H

#include <linux/types.h>

struct crypto_aead;

int awg_cookie_aead_create(struct crypto_aead **out);
void awg_cookie_aead_destroy(struct crypto_aead *aead);

int awg_xchacha20p1305_decrypt(struct crypto_aead *aead, const u8 key[32],
			       const u8 nonce_24[24],
			       const u8 *aad, size_t aad_len,
			       u8 *ct_with_tag, size_t ct_with_tag_len);

int awg_xchacha20p1305_encrypt(struct crypto_aead *aead, const u8 key[32],
			       const u8 nonce_24[24],
			       const u8 *aad, size_t aad_len,
			       u8 *pt_out_buf, size_t pt_len);

#endif /* _AWG_PROXY_COOKIE_H */

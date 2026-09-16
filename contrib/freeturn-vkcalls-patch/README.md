# VKCalls patch for hoaxisr/free-turn-proxy (1.8.0-3)

WDTT-style auth via `api.vk.me` before legacy Smart Captcha path.

## Apply

```bash
git clone -b awg https://github.com/hoaxisr/free-turn-proxy.git
cd free-turn-proxy
git am /path/to/0001-feat-vkauth-VKCalls-auth-path-before-legacy-captcha-.patch
```

Or copy files from `files/` into the matching paths in free-turn-proxy.

## PR target

- **Repo:** https://github.com/hoaxisr/free-turn-proxy
- **Base branch:** `awg`

## Env (optional)

- `FREETURN_VK_AUTH_MODE=legacy` — skip VKCalls
- `FREETURN_SKIP_VKCALLS=1` — same

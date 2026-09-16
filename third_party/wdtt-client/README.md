# WDTT / qWDTT router client (vendored)

Headless CLI from [amurcanov/proxy-turn-vk-android](https://github.com/amurcanov/proxy-turn-vk-android) `app/src/main/assets/android-client/`.

## Populate sources

```bash
git clone --depth 1 --filter=blob:none --sparse https://github.com/amurcanov/proxy-turn-vk-android.git _tmp
cd _tmp && git sparse-checkout set app/src/main/assets/android-client
cp -r app/src/main/assets/android-client/* ../
cd .. && rm -rf _tmp
```

Or on Windows (from this directory):

```bat
git clone --depth 1 --filter=blob:none --sparse https://github.com/amurcanov/proxy-turn-vk-android.git _tmp
cd _tmp && git sparse-checkout set app/src/main/assets/android-client
xcopy /E /I /Y app\src\main\assets\android-client\* ..
cd .. && rmdir /s /q _tmp
```

## Build (from awg-manager root)

```bash
./scripts/build-wdtt-client.sh        # all router arches
./scripts/build-wdtt-client.sh arm64   # aarch64 only
```

Output: `prebuilt/wdtt/client-linux-*`

Requires Go **1.26+**, `CGO_ENABLED=0`.

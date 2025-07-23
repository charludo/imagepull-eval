# How to use

1. build the imagepuller from within contrast and put the bin somewhere accessible:
    ```bash
    nix build .#imagepuller
    cp ./result/bin/imagepuller /path/to/here/imagepuller
    ```
2. build the cdh ttrpc server and image-rs and put the bin somewhere accessible:
    ```bash
    git clone git@github.com:confidential-containers/guest-components.git
    cd guest-components
    nix develop github:edgelesssys/contrast#kata
    cargo build -p confidential-data-hub --bin ttrpc-cdh --release
    cp ./target/release/ttrpc-cdh /path/to/here/image-rs
    ```
3. build the eval bin:
    ```bash
    go build main.go
    ```
4. run the evaluation:
    ```bash
    sudo ./main ./imagepuller ./image-rs
    ```
    Note: `sudo` is required to access the socket, for mounting and unmounting, cleaning up image-rs dirs,...

# Observations

- Individual tests (pull and mount one image, then wipe everything and restart ttrpc server):
    - "time taken" isn't really a useful metric here, since it's dominated by the time it takes to fetch the images via the network, which we have no control over
    - `imagepuller` seems to consistently require *slightly* less storage (few MB) than `image-rs`
    - `image-rs` seems to have more stable memory usage, usually in the 33MB-35MB range, where `imagepuller` fluctuates quite a bit (23MB-43MB); the fluctuations appear to *roughly* correlate with the size of the image.
- Continuous tests (start server once, pull all images, only wiping the mount point in between each pull, not the storage):
    - roughly the same results as above
    - however, `image-rs` now requires more memory than `imagepuller`, although the latter one's usage also increased slightly, and storage requirement differences have flipped

```
===== Testing server (individual): imagepuller =====
[dmesg:v0.0.1]
Time taken: 4.37s
Memory peak: 32.85 MB
Storage used: 88.23 MB

[coordinator]
Time taken: 6.46s
Memory peak: 39.55 MB
Storage used: 165.23 MB

[busybox]
Time taken: 1.85s
Memory peak: 23.80 MB
Storage used: 1.45 MB

[nginx-unprivileged]
Time taken: 10.33s
Memory peak: 42.39 MB
Storage used: 244.76 MB

[prometheus]
Time taken: 13.34s
Memory peak: 39.27 MB
Storage used: 261.21 MB

[initializer]
Time taken: 5.68s
Memory peak: 39.89 MB
Storage used: 137.08 MB

===== Testing server (individual): image-rs =====
[dmesg:v0.0.1]
Time taken: 4.11s
Memory peak: 34.07 MB
Storage used: 92.26 MB

[coordinator]
Time taken: 6.1s
Memory peak: 33.94 MB
Storage used: 169.00 MB

[busybox]
Time taken: 1.28s
Memory peak: 33.06 MB
Storage used: 7.58 MB

[nginx-unprivileged]
Time taken: 8.66s
Memory peak: 37.04 MB
Storage used: 245.74 MB

[prometheus]
Time taken: 11.1s
Memory peak: 34.05 MB
Storage used: 266.86 MB

[initializer]
Time taken: 5.43s
Memory peak: 33.75 MB
Storage used: 140.43 MB

===== Testing server (continuous): imagepuller =====
Time taken: 40.64s
Memory peak: 49.02 MB
Storage used: 897.78 MB

===== Testing server (continuous): image-rs =====
Time taken: 37.22s
Memory peak: 55.03 MB
Storage used: 889.91 MB
```

For images with many layers, memory usage of `imagepuller` jumps:
```
===== Testing server (individual): imagepuller =====
[tensorflow:latest-gpu]
Time taken: 6m14.41s
Memory peak: 73.26 MB
Storage used: 7276.26 MB

===== Testing server (individual): image-rs =====
[tensorflow:latest-gpu]
Time taken: 7m27.14s
Memory peak: 37.39 MB
Storage used: 7249.36 MB
```

For very large images,
```

```

# Notes unrelated to resource usage
- `image-rs` simply fails when trying to re-pull a pulled image, the error states that the `config.json` file already exists.
  In contrast (pun intended), the `imagepuller` complies, mounting over the existing mount.
- `imagepuller` returns an empty response upon successful pulls, `image-rs` returns the sha256 of the `config.json` file
- since we don't write a `config.json` at all, I'm not sure if we should even bother to align the behavior, since `kata-agent` seems to cope fine with the `imagepuller` behavior


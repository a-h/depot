# Signatures

The current Nix HTTP binary cache implementation doesn't have any behaviour around digital signatures.

It's expected that files will be signed, however, when downloading the current round trip test fails due to a lack of digital signatures from the store.

Rather than bypassing signature verification by using `--no-check-sigs`, I'd like signatures to be provided by the server, and verified by the CLI.

When I run `nix config show`, I can see some config:

```toml
trusted-public-keys = cache.nixos.org-1:6NCHdD59X431o0gWypbMrAURkbJ16ZPMQFGspcDShjY= cache.flakehub.com-3:hJuILl5sVK4iKm86JzgdXW12Y2Hwd5G07qKtHTOcDCM= cache.flakehub.com-4:Asi8qIv291s0aYLyH6IOnr5Kf6+OF14WVjkE6t3xMio= cache.flakehub.com-5:zB96CRlL7tiPtzA9/WKyPkp3A2vqxqgdgyTVNGShPDU= cache.flakehub.com-6:W4EGFwAGgBj3he7c5fNh9NkOXw0PUVaxygCVKeuvaqU= cache.flakehub.com-7:mvxJ2DZVHn/kRxlIaxYNMuDG1OvMckZu32um1TadOR8= cache.flakehub.com-8:moO+OVS0mnTjBTcOUh2kYLQEd59ExzyoW1QgQ8XAARQ= cache.flakehub.com-9:wChaSeTI6TeCuV/Sg2513ZIM9i0qJaYsF+lZCXg0J6o= cache.flakehub.com-10:2GqeNlIp6AKp4EF2MVbE1kBOp9iBSyo0UPR9KoR0o1Y=
```

For now, we should have a test private key and public key that's committed to the repo so that it's static, and test users can add the public key. In the future, we'll add a CLI feature to `depot` much like the Attic binary cache has `attic use`.

The `go-nix` package has some useful looking packages:

- Nix specific hashing - https://github.com/nix-community/go-nix/blob/main/pkg/nixhash/hash.go
- Nar utilities - https://github.com/nix-community/go-nix/tree/main/pkg/nar

The Rust based Nix cache Attic has signature support: https://github.com/zhaofengli/attic/blob/main/attic/src/signing/mod.rs#L30

It seems that it signs objects: https://github.com/zhaofengli/attic/blob/7c5d79ad62cda340cb8c80c99b921b7b7ffacf69/attic/src/signing/mod.rs#L149

https://github.com/zhaofengli/attic/blob/7c5d79ad62cda340cb8c80c99b921b7b7ffacf69/server/src/narinfo/mod.rs#L189

But I'm not sure what the expected behaviour is.

## Behaviour

When I run `curl https://cache.nixos.org/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg.narinfo`

I note that there's a signature section.

```narinfo
StorePath: /nix/store/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg-sl-5.05
URL: nar/1cq669mqpzdm7c3r411mj5452v8fn26f4p2knnxfa7rqccjh5a5f.nar.xz
Compression: xz
FileHash: sha256:1cq669mqpzdm7c3r411mj5452v8fn26f4p2knnxfa7rqccjh5a5f
FileSize: 7292
NarHash: sha256:01k25dsan4vya77pzr1wc7qhml3fqsgiqll29mv42va6l3a59q4m
NarSize: 54632
References: m7ys2iqah82aa0409qmgsnas4y0p53ci-ncurses-6.5
Deriver: 5kl200crr6r3hxmpwhcxxh8ql3f30v29-sl-5.05.drv
Sig: cache.nixos.org-1:1UeV8vUjgCTBPy1DNxEbsWxj4Y9bVFmkNjFAOiVWgszFocz25wvR7hdx81mxzt6La7bswWBHKFLubmpm4h4iBA==
```

However, when I copy `sl` to a locally running depot:

```bash
nix copy --to http://localhost:8080 --refresh /nix/store/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg-sl-5.05
```

Then get the result:

```bash
curl http://localhost:8080/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg.narinfo
```

No signature is returned:

```narinfo
StorePath: /nix/store/4h86fqf4nl9l4dqj8sjvqfw0f9x22wpg-sl-5.05
URL: nar/1cq669mqpzdm7c3r411mj5452v8fn26f4p2knnxfa7rqccjh5a5f.nar.xz
Compression: xz
FileHash: sha256:1cq669mqpzdm7c3r411mj5452v8fn26f4p2knnxfa7rqccjh5a5f
FileSize: 7292
NarHash: sha256:01k25dsan4vya77pzr1wc7qhml3fqsgiqll29mv42va6l3a59q4m
NarSize: 54632
References: m7ys2iqah82aa0409qmgsnas4y0p53ci-ncurses-6.5
Deriver: 5kl200crr6r3hxmpwhcxxh8ql3f30v29-sl-5.05.drv
```

I guess this means that either the PUT operation or GET operation should have signed the result, but this hasn't happened.

## Server config

The server will probably need to support being passed a keypair of some sort to sign everything. This should probably be a file or files on disk rather than an environment variable, since later, a secret will probably be mounted to disk in k8s.

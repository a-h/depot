{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-25.11";
    gitignore = {
      url = "github:hercules-ci/gitignore.nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    version = {
      url = "github:a-h/version";
      inputs.nixpkgs.follows = "nixpkgs";
    };
    xc = {
      url = "github:joerdav/xc";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { self, nixpkgs, gitignore, version, xc, }:
    let
      allSystems = [
        "x86_64-linux" # 64-bit Intel/AMD Linux
        "aarch64-linux" # 64-bit ARM Linux
        "x86_64-darwin" # 64-bit Intel macOS
        "aarch64-darwin" # 64-bit ARM macOS
      ];

      forAllSystems = f: nixpkgs.lib.genAttrs allSystems (system: f {
        system = system;
        pkgs = import nixpkgs {
          inherit system;
          overlays = [
            (final: prev: {
              xc = xc.outputs.packages.${system}.xc;
              version = version.outputs.packages.${system}.default;
              python3WithPip = prev.python3.withPackages (ps: [
                ps.pip
              ]);
            })
          ];
        };
      });

      v = builtins.readFile ./.version;

      # Build app.
      app = { name, pkgs, system, }: pkgs.buildGoModule {
        pname = name;
        version = v;
        src = gitignore.lib.gitignoreSource ./.;
        vendorHash = "sha256-R/yWffKtZE7noydIKRV/DK0y5Nhh/HRGLjHeQPF04XU=";
        subPackages = [ "cmd/${name}" ];
        ldflags = [
          "-s"
          "-w"
        ];
        # Skip tests, we run those as part of CI.
        doCheck = false;
      };

      # Build Docker containers.
      dockerUser = pkgs:
        pkgs.runCommand "user" { } ''
          mkdir -p $out/etc
          echo "user:x:1000:1000:user:/home/user:/bin/false" > $out/etc/passwd
          echo "user:x:1000:" > $out/etc/group
          echo "user:!:1::::::" > $out/etc/shadow
        '';
      dockerImage = { name, pkgs, system, }:
        pkgs.dockerTools.buildLayeredImage {
          name = "ghcr.io/a-h/depot";
          tag = "latest";
          contents = [
            pkgs.coreutils
            pkgs.bash
            pkgs.cacert
            pkgs.dockerTools.caCertificates
            pkgs.sqlite
            (dockerUser pkgs)
            (app { inherit name pkgs system; })
          ];
          # The config attribute maps to Docker's container config JSON:
          # https://docs.docker.com/engine/api/v1.41/#operation/ImageBuild
          config = {
            Cmd = [ "depot" "serve" ];
            Env = [ "DEPOT_STORE=/depot-store" ];
            User = "user:user";
            ExposedPorts = {
              "8080/tcp" = { };
            };
          };
        };

      # Development tools used.
      devTools = pkgs: [
        pkgs.crane
        pkgs.gh
        pkgs.git
        pkgs.go
        pkgs.version
        pkgs.xc

        # Used to test the depot system.
        pkgs.nodejs
        pkgs.python3WithPip
        pkgs.minio
      ];

      name = "depot";
    in
    {
      # `nix build` builds the app.
      # `nix build .#docker-image` builds the Docker container.
      # You will need a Linux system to build the Docker container.
      # e.g. nix build .#packages.x86_64-linux.docker-image
      packages = forAllSystems ({ system, pkgs }: rec {
        default = app { name = name; pkgs = pkgs; system = system; };
        depot = default;
      }
      // (if pkgs.stdenv.isLinux then {
        docker-image = dockerImage { name = name; pkgs = pkgs; system = system; };
      } else { }));

      # `nix develop` provides a shell containing required tools.
      devShells = forAllSystems ({ system, pkgs }: {
        default = pkgs.mkShell {
          buildInputs = (devTools pkgs);
        };
      });
    };
}

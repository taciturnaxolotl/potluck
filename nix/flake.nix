{
  description = "potluck — pooled chat frontend over pioneer.ai";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    let
      systems = [ "aarch64-linux" "x86_64-linux" "aarch64-darwin" "x86_64-darwin" ];
    in
    flake-utils.lib.eachSystem systems (system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        packages.default = pkgs.buildGoModule {
          pname = "potluck";
          version = "0.0.1";
          src = ./server;
          # Update on dependency change: nix build will print expected hash.
          vendorHash = null;
          subPackages = [ "cmd/server" ];
          ldflags = [ "-s" "-w" ];
          # CGO-free: modernc.org/sqlite is pure Go.
          CGO_ENABLED = "0";
        };
        packages.server = self.packages.${system}.default;

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            go-task
            sqlc
            goose
            air
            bun
            nodejs
          ];
        };
      })
    // {
      nixosModules.default = import ./module.nix self;
      nixosModules.potluck = self.nixosModules.default;
    };
}

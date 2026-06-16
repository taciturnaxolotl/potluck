{
  description = "potluck — pooled chat frontend over pioneer.ai";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "aarch64-linux"
        "x86_64-linux"
        "aarch64-darwin"
        "x86_64-darwin"
      ];
      forEachSystem = nixpkgs.lib.genAttrs systems;
    in
    {
      packages = forEachSystem (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          server = pkgs.buildGoModule {
            pname = "potluck";
            version = "0.0.1";
            src = ./server;
            vendorHash = "sha256-gWeY3ZoOZNObT9LjWEuRijY2S48Gm36bGpK88mohmSE=";
            subPackages = [ "cmd/server" ];
            ldflags = [
              "-s"
              "-w"
            ];
            env.CGO_ENABLED = "0";
          };
        in
        {
          default = server;
          inherit server;
        }
      );

      devShells = forEachSystem (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShell {
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
        }
      );

    };
}

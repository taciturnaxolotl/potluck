# NixOS module for potluck.
#
# Wraps the server binary as a systemd unit. Intended to be consumed via
# the flake's `nixosModules.default`. Pair with an agenix secret that
# defines the EnvironmentFile.
self: { config, lib, pkgs, ... }:
let
  cfg = config.services.potluck;
in
{
  options.services.potluck = {
    enable = lib.mkEnableOption "potluck — pooled pioneer.ai chat frontend";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      description = "potluck server package.";
    };

    port = lib.mkOption {
      type = lib.types.port;
      default = 8080;
      description = "TCP port the server listens on.";
    };

    dataDir = lib.mkOption {
      type = lib.types.path;
      default = "/var/lib/potluck";
      description = "Directory holding the SQLite database.";
    };

    user = lib.mkOption {
      type = lib.types.str;
      default = "potluck";
      description = "System user the service runs as.";
    };

    group = lib.mkOption {
      type = lib.types.str;
      default = "potluck";
      description = "System group the service runs as.";
    };

    environmentFile = lib.mkOption {
      type = lib.types.nullOr lib.types.path;
      default = null;
      description = ''
        Path to an EnvironmentFile defining secrets like PIONEER_API_KEY,
        POTLUCK_SESSION_SECRET, and the LITESTREAM_B2_* trio. Best supplied
        via agenix.
      '';
    };
  };

  config = lib.mkIf cfg.enable {
    users.users.${cfg.user} = {
      isSystemUser = true;
      group = cfg.group;
      home = cfg.dataDir;
      createHome = true;
    };
    users.groups.${cfg.group} = { };

    systemd.services.potluck = {
      description = "potluck — pooled pioneer.ai chat frontend";
      after = [ "network.target" ];
      wantedBy = [ "multi-user.target" ];

      environment = {
        POTLUCK_ADDR = ":${toString cfg.port}";
        POTLUCK_DB = "${cfg.dataDir}/potluck.db";
      };

      serviceConfig = {
        ExecStart = "${cfg.package}/bin/server";
        WorkingDirectory = cfg.dataDir;
        User = cfg.user;
        Group = cfg.group;
        Restart = "on-failure";
        RestartSec = 2;
        StateDirectory = "potluck";

        # hardening
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ReadWritePaths = [ cfg.dataDir ];
      } // lib.optionalAttrs (cfg.environmentFile != null) {
        EnvironmentFile = cfg.environmentFile;
      };
    };
  };
}

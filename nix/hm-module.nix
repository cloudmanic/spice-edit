# =============================================================================
# File: nix/hm-module.nix
# Author: Luis Quiñones <lpaandres2020@gmail.com>
# Created: 2026-07-23
# Copyright: 2026 Cloudmanic, LLC. All rights reserved.
# =============================================================================
{
  config,
  lib,
  pkgs,
  ...
}: let
  cfg = config.programs.spiceedit;
  json = pkgs.formats.json {};

  inherit (lib) mkEnableOption mkIf mkOption mkPackageOption types;

  promptType = types.submodule {
    options = {
      key = mkOption {
        type = types.strMatching "[A-Z_][A-Z0-9_]*";
        description = "Env var the command reads. Uppercase/underscore/digits, not leading digit.";
      };

      label = mkOption {type = types.str;};

      type = mkOption {
        type = types.enum ["text" "select"];
        default = "text";
      };

      options = mkOption {
        type = types.listOf types.str;
        default = [];
        description = "Choices for a select prompt. Required when type = \"select\".";
      };

      default = mkOption {
        type = types.str;
        default = "";
        description = "Seed value; may reference editor vars like \${ACTIVE_FOLDER}.";
      };
    };
  };

  actionType = types.submodule {
    options = {
      label = mkOption {type = types.str;};

      command = mkOption {
        type = types.str;
        description = "Shell command. Expands \$FILE, \$FILENAME, \$HOST and any prompt keys.";
      };

      prompts = mkOption {
        type = types.listOf promptType;
        default = [];
      };
    };
  };
in {
  options.programs.spiceedit = {
    enable = mkEnableOption "SpiceEdit, an opinionated mouse-first terminal code editor";

    package = mkPackageOption pkgs "spiceedit" {};

    icons = mkOption {
      type = types.enum ["auto" "on" "off"];
      default = "auto";
      description = ''
        Nerd Font icons in the file tree. "auto" detects a Nerd Font at
        startup; "on"/"off" bypass detection. Rendered to config.json.
      '';
    };

    actions = mkOption {
      type = types.listOf actionType;
      default = [];
      description = "Custom shell-out entries in the ≡ menu, rendered to actions.json.";
      example = lib.literalExpression ''
        [
          {
            label = "Commit";
            command = "cd \"$ACTIVE_FOLDER\" && git commit -m \"$MSG\"";
            prompts = [
              {
                key = "MSG";
                label = "Message";
                type = "text";
              }
            ];
          }
        ]
      '';
    };

    formatters = mkOption {
      type = types.attrsOf (types.listOf types.str);
      default = {};
      description = ''
        Global format-on-save defaults keyed by file extension (no dot),
        rendered to format-defaults.json. Use $FILE for the file path.
      '';
      example = lib.literalExpression ''
        {
          go = ["gofmt" "-w" "$FILE"];
          json = ["prettier" "--write" "$FILE"];
        }
      '';
    };
  };

  config = mkIf cfg.enable {
    home.packages = [cfg.package];

    xdg.configFile = lib.mkMerge [
      {
        "spiceedit/config.json".source =
          json.generate "config.json" {icons = cfg.icons;};
      }

      (mkIf (cfg.actions != []) {
        "spiceedit/actions.json".source =
          json.generate "actions.json" {actions = cfg.actions;};
      })

      (mkIf (cfg.formatters != {}) {
        "spiceedit/format-defaults.json".source =
          json.generate "format-defaults.json" {commands = cfg.formatters;};
      })
    ];
  };
}

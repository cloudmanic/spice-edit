# =============================================================================
# File: flake.nix
# Author: Luis Quiñones <lpaandres2020@gmail.com>
# Created: 2026-07-23
# Copyright: 2026 Cloudmanic, LLC. All rights reserved.
# =============================================================================
{
  description = "Opinionated mouse-first terminal code editor";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = {
    self,
    nixpkgs,
  }: let
    systems = ["x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin"];
    forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
  in {
    packages = forAllSystems (pkgs: rec {
      spiceedit = pkgs.callPackage ./nix/package.nix {};
      default = spiceedit;
    });

    overlays.default = _final: prev: {
      spiceedit = prev.callPackage ./nix/package.nix {};
    };

    homeModules = rec {
      spiceedit = {pkgs, ...}: {
        imports = [./nix/hm-module.nix];
        programs.spiceedit.package =
          nixpkgs.lib.mkDefault self.packages.${pkgs.stdenv.hostPlatform.system}.spiceedit;
      };
      default = spiceedit;
    };

    formatter = forAllSystems (pkgs: pkgs.alejandra);
  };
}

# =============================================================================
# File: nix/package.nix
# Author: Luis Quiñones <lpaandres2020@gmail.com>
# Created: 2026-07-23
# Copyright: 2026 Cloudmanic, LLC. All rights reserved.
# =============================================================================
{
  lib,
  buildGoModule,
}:
buildGoModule {
  pname = "spiceedit";
  version = let
    raw = builtins.readFile ../internal/version/version.go;
    m = builtins.match ''.*Version = "([^"]+)".*'' (builtins.replaceStrings ["\n"] [" "] raw);
  in
    builtins.head m;

  src = lib.fileset.toSource {
    root = ../.;
    fileset = lib.fileset.unions [
      ../go.mod
      ../go.sum
      ../main.go
      ../internal
    ];
  };

  vendorHash = "sha256-rjmk+9Yz3riXfvCERs6noGuVOFyEt8SoHbxjAt7D2IY=";

  env.CGO_ENABLED = 0;
  ldflags = ["-s" "-w"];

  postInstall = ''
    mv $out/bin/spice-edit $out/bin/spiceedit
  '';

  meta = {
    description = "Opinionated mouse-first terminal code editor";
    homepage = "https://github.com/cloudmanic/spice-edit";
    license = lib.licenses.mit;
    mainProgram = "spiceedit";
  };
}

# Copyright 2026 Humaid Alqasimi
# SPDX-License-Identifier: Apache-2.0
{ inputs, ... }:
{
  imports = [ inputs.git-hooks-nix.flakeModule ];
  perSystem =
    {
      config,
      self',
      lib,
      ...
    }:
    {
      checks = lib.mapAttrs' (n: lib.nameValuePair "package-${n}") self'.packages;

      pre-commit = {
        settings = {
          hooks = {
            treefmt = {
              enable = true;
              package = config.treefmt.build.wrapper;
            };
          };
        };

      };
    };
}

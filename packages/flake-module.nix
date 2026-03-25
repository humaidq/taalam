# Copyright 2026 Humaid Alqasimi
# SPDX-License-Identifier: Apache-2.0
{ self, ... }:
let
  buildCommit =
    self.shortRev or (
      if self ? rev then
        builtins.substring 0 7 self.rev
      else
        self.dirtyShortRev or (if self ? dirtyRev then builtins.substring 0 7 self.dirtyRev else "unknown")
    );
in
{
  perSystem =
    { pkgs, ... }:
    let
      taalamPackage = pkgs.callPackage ../src {
        inherit buildCommit;
      };
    in
    {
      packages = {
        default = taalamPackage;
        taalam = taalamPackage;
      };
    };
}

# Copyright 2026 Humaid Alqasimi
# SPDX-License-Identifier: Apache-2.0
{
  lib,
  pkgs,
  buildCommit ? "unknown",
  ...
}:

pkgs.buildGoModule rec {
  pname = "taalam";
  version = "v0.1.1";

  src = ./.;

  # use vendor has null to avoid creating a Fixed-Output derivation
  # if using the devshell the go-update will ensure that
  # `go mod vendor` is run to keep the vendor directory up to date
  # this is tracked so it will give the reproducibility of the build
  vendorHash = null;

  ldflags = [
    "-X github.com/humaidq/taalam/cmd.BuildVersion=${version}"
    "-X github.com/humaidq/taalam/cmd.BuildCommit=${buildCommit}"
  ];

  nativeBuildInputs = [ pkgs.makeWrapper ];

  postFixup = ''
    wrapProgram "$out/bin/taalam" \
      --prefix PATH : "${lib.makeBinPath [ pkgs.nix-search-cli ]}"
  '';
}

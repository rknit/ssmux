{
  description = "ssmux: tmux session switcher";

  inputs = {
    nixpkgs.url = "https://channels.nixos.org/nixpkgs-unstable/nixexprs.tar.zst";
  };

  outputs = inputs: {
    packages = builtins.mapAttrs (system: pkgs: {
      ssmux = (pkgs.buildGoModule.override { go = pkgs.go_1_27; }) rec {
        pname = "ssmux";
        version = "1.5.0"; # x-release-please-version
        src = ./.;
        ldflags = [
          "-X github.com/rknit/ssmux/internal/app.Version=${version}"
        ];
        vendorHash = "sha256-ehI0eDXfeywbxf7nwji1jPVDVaiFU34SF+DhqCA2cGQ=";
        # ncurses provides infocmp, which wrapper tests run to check terminfo
        nativeCheckInputs = [ pkgs.tmux pkgs.ncurses ];
        # tests start tmux servers on unix sockets
        __darwinAllowLocalNetworking = true;
      };

      default = inputs.self.packages.${system}.ssmux;
    }) inputs.nixpkgs.legacyPackages;

    # the package with its tests run under the race detector; built by CI
    checks = builtins.mapAttrs (system: _: {
      race = inputs.self.packages.${system}.ssmux.overrideAttrs (_: {
        checkFlags = [ "-race" ];
      });
    }) inputs.nixpkgs.legacyPackages;

    devShells = builtins.mapAttrs (system: pkgs: {
      default = pkgs.mkShell {
        packages = [
          pkgs.go_1_27
          pkgs.gopls
          pkgs.tmux
        ];
      };
    }) inputs.nixpkgs.legacyPackages;
  };
}

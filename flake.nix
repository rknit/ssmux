{
  description = "ssmux: tmux session switcher";

  inputs = {
    nixpkgs.url = "https://channels.nixos.org/nixpkgs-unstable/nixexprs.tar.zst";
  };

  outputs = inputs: {
    packages = builtins.mapAttrs (system: pkgs: {
      ssmux = (pkgs.buildGoModule.override { go = pkgs.go_1_27; }) {
        pname = "ssmux";
        version = "0.1.0";
        src = ./.;
        vendorHash = "sha256-ehI0eDXfeywbxf7nwji1jPVDVaiFU34SF+DhqCA2cGQ=";
        nativeCheckInputs = [ pkgs.tmux ];
        # tests start tmux servers on unix sockets
        __darwinAllowLocalNetworking = true;
      };

      default = inputs.self.packages.${system}.ssmux;
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

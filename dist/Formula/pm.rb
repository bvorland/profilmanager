# Homebrew formula for profilmanager (pm).
#
# This is a bottle-less formula that downloads the GoReleaser-built
# release tarball for the user's platform. Placeholders to substitute
# when cutting a release (or have GoReleaser auto-PR via `brews:`):
#
#   <VERSION>           -> release semver, e.g. 0.1.0 (no leading "v")
#   <SHA256_DARWIN_X64> -> sha256 of pm_<VERSION>_darwin_amd64.tar.gz
#   <SHA256_DARWIN_ARM> -> sha256 of pm_<VERSION>_darwin_arm64.tar.gz
#   <SHA256_LINUX_X64>  -> sha256 of pm_<VERSION>_linux_amd64.tar.gz
#   <SHA256_LINUX_ARM>  -> sha256 of pm_<VERSION>_linux_arm64.tar.gz
#
# Drop the rendered file into a `Formula/` directory of a
# `homebrew-tap` repository (e.g. bvorland/homebrew-tap) so users can:
#
#   brew install bvorland/tap/pm

class Pm < Formula
  desc "Multi-environment developer profile manager"
  homepage "https://github.com/bvorland/profilmanager"
  version "<VERSION>"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/bvorland/profilmanager/releases/download/v#{version}/pm_#{version}_darwin_arm64.tar.gz"
      sha256 "<SHA256_DARWIN_ARM>"
    end
    on_intel do
      url "https://github.com/bvorland/profilmanager/releases/download/v#{version}/pm_#{version}_darwin_amd64.tar.gz"
      sha256 "<SHA256_DARWIN_X64>"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/bvorland/profilmanager/releases/download/v#{version}/pm_#{version}_linux_arm64.tar.gz"
      sha256 "<SHA256_LINUX_ARM>"
    end
    on_intel do
      url "https://github.com/bvorland/profilmanager/releases/download/v#{version}/pm_#{version}_linux_amd64.tar.gz"
      sha256 "<SHA256_LINUX_X64>"
    end
  end

  def install
    bin.install "pm"
  end

  def caveats
    <<~EOS
      To let `pm session` mutate your current shell, add the appropriate
      line to your shell rc file:

        bash:  eval "$(pm session init --shell bash)"
        zsh:   eval "$(pm session init --shell zsh)"
        fish:  pm session init --shell fish | source

      See: https://github.com/bvorland/profilmanager/blob/main/dist/INSTALL.md
    EOS
  end

  test do
    assert_match "pm", shell_output("#{bin}/pm version")
  end
end

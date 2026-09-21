# Homebrew formula for remainder.
#
# Intended home: Formula/remainder.rb in a douglasjarquin/homebrew-tap
# repository, installed as `brew install douglasjarquin/tap/remainder`.
# Until that tap exists it can be installed from a checkout with
# `brew install ./homebrew/remainder.rb`.
#
# Remainder publishes one archive per platform rather than one tag for every
# platform, so each block below pins the newest release carrying its target.
class Remainder < Formula
  desc "One-shot quota CLI for local AI provider evidence"
  homepage "https://github.com/douglasjarquin/remainder"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/douglasjarquin/remainder/releases/download/v0.3.0/remainder_v0.3.0_darwin_arm64.tar.gz"
      sha256 "b0e263a0f0ac96125d76691cf145836650d777cfd7a85dd8d840ae5c1a898ba2"
      version "0.3.0"
    end
    on_intel do
      odie "remainder publishes no Intel Mac archive; use scripts/install.sh or build from source"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/douglasjarquin/remainder/releases/download/v0.2.1/remainder_v0.2.1_linux_arm64.tar.gz"
      sha256 "fcd32077532ba917689efda60153a63fcd71a6cf78cd6b6cc76d17c8b309a7e8"
      version "0.2.1"
    end
    on_intel do
      url "https://github.com/douglasjarquin/remainder/releases/download/v0.2.2/remainder_v0.2.2_linux_amd64.tar.gz"
      sha256 "7828125a2bdb9b99d1a23d27f9ab1ffd1eaf0dce3763724bbf7604fade4346c7"
      version "0.2.2"
    end
  end

  def install
    target = "#{OS.mac? ? "darwin" : "linux"}_#{Hardware::CPU.arm? ? "arm64" : "amd64"}"
    bundle = "remainder_v#{version}_#{target}"
    binary = File.exist?("#{bundle}/bin/remainder") ? "#{bundle}/bin/remainder" : "#{bundle}/remainder"
    bin.install binary
  end

  test do
    assert_match "remainder v#{version}", shell_output("#{bin}/remainder --version")
  end
end

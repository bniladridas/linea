class Linea < Formula
  desc "Local-first AI assistant"
  homepage "https://github.com/bniladridas/linea"
  url "https://github.com/bniladridas/linea/archive/refs/tags/v0.3.15.tar.gz"
  sha256 "cf4bdb08d1b7270dbc40c9157cbae7e7e6939149017c428ebaab6464c18d5289"
  license "MIT"
  head "https://github.com/bniladridas/linea.git", branch: "main"

  bottle do
    root_url "https://github.com/bniladridas/linea/releases/download/v0.3.15"
    sha256 arm64_sequoia: "f8a114cb226051a65120d4b7de3ad9ac8cf6f027686bc4a7886be7328a2e639b"
  end

  depends_on "go" => :build
  depends_on "node" => :build
  depends_on "postgresql@16"

  def install
    cd "frontend" do
      system "npm", "ci"
      system "npm", "run", "build"
    end

    cd "backend" do
      system "go", "build", *std_go_args(
        output:  bin/"linea",
        ldflags: "-s -w -X main.version=#{version}",
      ), "./cmd/server"
    end
  end

  service do
    run [opt_bin/"linea"]
    keep_alive true
    working_dir HOMEBREW_PREFIX
  end

  def caveats
    <<~EOS
      Create ~/.config/linea/linea.env with local configuration:

        API_ADDR=127.0.0.1:8080
        DATABASE_URL=postgres://linea:linea@localhost:5432/linea?sslmode=disable
        GEMINI_API_KEY=your-key
        GEMINI_MODEL=gemini-2.5-flash-lite

      Initialize or upgrade the database:

        set -a; . ~/.config/linea/linea.env; set +a
        linea -migrate

      Run Linea:

        linea

      Or start it as a service:

        brew services start linea
    EOS
  end

  test do
    assert_match "linea #{version}", shell_output("#{bin}/linea -version")
  end
end

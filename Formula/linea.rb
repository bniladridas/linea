class Linea < Formula
  desc "Local-first AI assistant"
  homepage "https://github.com/bniladridas/linea"
  url "https://github.com/bniladridas/linea/archive/refs/tags/v0.3.10.tar.gz"
  sha256 "c6c7518f50ff02f512b7f43bab91889522f48ed304a556f4056bd250c92d3c66"
  license "MIT"
  head "https://github.com/bniladridas/linea.git", branch: "main"

  bottle do
    root_url "https://github.com/bniladridas/linea/releases/download/v0.3.10"
    sha256 arm64_sequoia: "77e2fcea0e77391357dc7dbe4d0d8b93bc59984667df656c4921fd4c14934063"
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

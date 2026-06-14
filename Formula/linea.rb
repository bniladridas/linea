class Linea < Formula
  desc "Local-first AI assistant"
  homepage "https://github.com/bniladridas/linea"
  url "https://github.com/bniladridas/linea/archive/refs/tags/v0.3.11.tar.gz"
  sha256 "ee853c9b11814a30a3a579a7e22b349fe9df3153ef50f57045e1ffeca646bac1"
  license "MIT"
  head "https://github.com/bniladridas/linea.git", branch: "main"

  bottle do
    root_url "https://github.com/bniladridas/linea/releases/download/v0.3.11"
    sha256 arm64_sequoia: "e9774f3d15cd8d61b00a42ceda6441c95e8595e80764c5d111a2992c6667c5e4"
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

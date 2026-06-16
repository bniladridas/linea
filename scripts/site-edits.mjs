import { execFileSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, relative } from "node:path";

const root = process.cwd();
const outputPath = "site/edits/index.html";
const ignored = new Set([outputPath]);

function git(args, options = {}) {
  return execFileSync("git", args, {
    cwd: root,
    encoding: "utf8",
    stdio: options.quiet ? ["ignore", "pipe", "ignore"] : ["ignore", "pipe", "pipe"],
  }).trimEnd();
}

function maybeGit(args) {
  try {
    return git(args, { quiet: true });
  } catch {
    return "";
  }
}

function escapeHtml(value) {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}

function changedHtmlFiles(commit) {
  return git(["diff-tree", "--no-commit-id", "--name-only", "-r", commit])
    .split("\n")
    .filter((file) => file.startsWith("site/"))
    .filter((file) => file.endsWith(".html"))
    .filter((file) => !ignored.has(file));
}

function commitParent(commit) {
  const parts = git(["rev-list", "--parents", "-n", "1", commit]).split(" ");
  return parts[1] ?? "4b825dc642cb6eb9a060e54bf8d69288fbee4904";
}

function diffFor(parent, commit, file) {
  const before = pageText(parent, file);
  const after = pageText(commit, file);
  return textDiff(before, after);
}

function decodeEntities(value) {
  return value
    .replaceAll("&nbsp;", " ")
    .replaceAll("&amp;", "&")
    .replaceAll("&lt;", "<")
    .replaceAll("&gt;", ">")
    .replaceAll("&quot;", '"')
    .replaceAll("&#39;", "'");
}

function visibleHtml(html) {
  const article = html.match(/<article[\s\S]*?<\/article>/i);
  if (article) return article[0];

  const sections = [...html.matchAll(/<section[\s\S]*?<\/section>/gi)].map((match) => match[0]);
  return sections.join("\n");
}

function pageText(revision, file) {
  const html = maybeGit(["show", `${revision}:${file}`]);
  if (!html) return [];

  return decodeEntities(
    visibleHtml(html)
      .replaceAll(/<svg[\s\S]*?<\/svg>/gi, "")
      .replaceAll(/<code>([\s\S]*?)<\/code>/gi, "`$1`")
      .replaceAll(/<\/(h1|h2|h3|p|span|a|section|article)>/gi, "\n")
      .replaceAll(/<[^>]+>/g, "")
  )
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
}

function textDiff(before, after) {
  const rows = Array.from({ length: before.length + 1 }, () => Array(after.length + 1).fill(0));

  for (let i = before.length - 1; i >= 0; i -= 1) {
    for (let j = after.length - 1; j >= 0; j -= 1) {
      rows[i][j] = before[i] === after[j]
        ? rows[i + 1][j + 1] + 1
        : Math.max(rows[i + 1][j], rows[i][j + 1]);
    }
  }

  const lines = [];
  let i = 0;
  let j = 0;
  while (i < before.length && j < after.length) {
    if (before[i] === after[j]) {
      lines.push(` ${before[i]}`);
      i += 1;
      j += 1;
    } else if (rows[i + 1][j] >= rows[i][j + 1]) {
      lines.push(`-${before[i]}`);
      i += 1;
    } else {
      lines.push(`+${after[j]}`);
      j += 1;
    }
  }

  while (i < before.length) {
    lines.push(`-${before[i]}`);
    i += 1;
  }

  while (j < after.length) {
    lines.push(`+${after[j]}`);
    j += 1;
  }

  return compactDiff(lines);
}

function compactDiff(lines) {
  const changedIndexes = lines
    .map((line, index) => (line.startsWith("+") || line.startsWith("-") ? index : -1))
    .filter((index) => index >= 0);

  if (changedIndexes.length === 0) return [];

  const keep = new Set();
  for (const index of changedIndexes) {
    for (let offset = -2; offset <= 2; offset += 1) {
      const keptIndex = index + offset;
      if (keptIndex >= 0 && keptIndex < lines.length) {
        keep.add(keptIndex);
      }
    }
  }

  const compacted = [];
  let previous = -1;
  for (const index of [...keep].sort((a, b) => a - b)) {
    if (previous >= 0 && index > previous + 1) {
      compacted.push("...");
    }
    compacted.push(lines[index]);
    previous = index;
  }

  return compacted;
}

function diffLineClass(line) {
  if (line.startsWith("+") && !line.startsWith("+++")) return "diff-add";
  if (line.startsWith("-") && !line.startsWith("---")) return "diff-remove";
  return "diff-context";
}

function renderDiff(lines) {
  return lines
    .filter((line) => line !== "\\ No newline at end of file")
    .map((line) => {
      const cls = diffLineClass(line);
      return `<span class="${cls}">${escapeHtml(line || " ")}</span>`;
    })
    .join("\n");
}

function renderPath(file) {
  return relative("site", file).replaceAll("\\", "/");
}

const commits = git(["log", "--reverse", "--format=%H%x09%ad%x09%s", "--date=short", "--", "site"])
  .split("\n")
  .filter(Boolean)
  .map((line) => {
    const [hash, date, ...subject] = line.split("\t");
    return { hash, shortHash: hash.slice(0, 7), date, subject: subject.join("\t") };
  });

const entries = [];
for (const commit of commits) {
  const files = changedHtmlFiles(commit.hash);
  if (files.length === 0) continue;

  const parent = commitParent(commit.hash);
  const fileEntries = files
    .map((file) => ({ file, diffLines: diffFor(parent, commit.hash, file) }))
    .filter((file) => file.diffLines.length > 0)
    .map((file) => ({ file: file.file, diff: renderDiff(file.diffLines) }));

  if (fileEntries.length === 0) continue;

  entries.push({ ...commit, files: fileEntries });
}

const editBlocks = entries.reverse().map((entry) => {
  const fileBlocks = entry.files.map((file) => `
            <section class="diff-card" aria-label="${escapeHtml(renderPath(file.file))}">
              <h3>${escapeHtml(renderPath(file.file))}</h3>
              <pre class="diff"><code>${file.diff}</code></pre>
            </section>`).join("\n");

  return `
          <section class="edit" aria-labelledby="edit-${entry.shortHash}">
            <h2 id="edit-${entry.shortHash}">${escapeHtml(entry.subject)}</h2>
            <p class="edit-meta">
              ${escapeHtml(entry.date)} ·
              <a href="https://github.com/bniladridas/linea/commit/${entry.hash}">${entry.shortHash}</a>
            </p>
${fileBlocks}
          </section>`;
}).join("\n");

const page = `<!doctype html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <meta name="description" content="Linea page edits.">
    <title>Edits</title>
    <link rel="stylesheet" href="../styles.css">
    <link
      rel="icon"
      href="data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 24 24' fill='none' stroke='%23f0f0f0' stroke-width='1.5' stroke-linecap='round' stroke-linejoin='round'%3E%3Crect width='24' height='24' rx='6' fill='%23151514' stroke='none'/%3E%3Ccircle cx='6' cy='19' r='3'/%3E%3Cpath d='M9 19h8.5a3.5 3.5 0 0 0 0-7h-11a3.5 3.5 0 0 1 0-7H15'/%3E%3Ccircle cx='18' cy='5' r='3'/%3E%3C/svg%3E"
    >
  </head>
  <body>
    <main class="page">
      <header class="site-header" aria-label="Linea">
        <a class="home-link" href="../index.html">
          <svg class="mark" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
            <circle cx="6" cy="19" r="3"></circle>
            <path d="M9 19h8.5a3.5 3.5 0 0 0 0-7h-11a3.5 3.5 0 0 1 0-7H15"></path>
            <circle cx="18" cy="5" r="3"></circle>
          </svg>
          <span>Linea</span>
        </a>
        <button id="tbtn" class="theme-toggle" aria-label="Toggle theme"></button>
      </header>

      <article class="article">
        <p class="kicker">Generated from Git history</p>
        <h1>Edits</h1>

        <div class="edits">
${editBlocks || '          <p>No page edits yet.</p>'}
        </div>
      </article>
    </main>
    <script src="../theme.js"></script>
  </body>
</html>
`;

mkdirSync(dirname(outputPath), { recursive: true });
writeFileSync(outputPath, page);

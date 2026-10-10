import { fileName, formatDuration } from "./model";
import { countOutcomes, type FileOutcome, type Report, type ReportFile } from "./report";

const outcomeLabels: Record<FileOutcome, string> = {
  succeeded: "成功",
  failed: "失败",
  cancelled: "已停止",
  notRun: "未执行",
  disabled: "已禁用",
};

const runLabels: Record<Report["outcome"], string> = {
  succeeded: "全部成功",
  failed: "执行失败",
  cancelled: "已停止",
  partial: "部分执行",
};

function esc(value: string | number | undefined) {
  return String(value ?? "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function clock(ms: number) {
  const date = new Date(ms);
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

function duration(file: ReportFile) {
  if (file.startedAt === undefined) return "—";
  return formatDuration((file.finishedAt ?? file.startedAt) - file.startedAt);
}

function kpis(report: Report) {
  const counts = countOutcomes(report);
  const total = report.files.length;
  const statements = report.files.reduce((sum, file) => sum + file.statements, 0);
  const executed = report.files.reduce((sum, file) => sum + file.executed, 0);
  const rows = report.files.reduce((sum, file) => sum + file.rows, 0);
  const segments = (Object.keys(outcomeLabels) as FileOutcome[])
    .filter((outcome) => counts[outcome] > 0)
    .map(
      (outcome) =>
        `<span class="seg ${outcome}" style="flex:${counts[outcome]}" title="${outcomeLabels[outcome]} ${counts[outcome]}"></span>`,
    )
    .join("");
  const legend = (Object.keys(outcomeLabels) as FileOutcome[])
    .filter((outcome) => counts[outcome] > 0)
    .map(
      (outcome) =>
        `<span class="legend"><i class="dot ${outcome}"></i>${outcomeLabels[outcome]} <b>${counts[outcome]}</b></span>`,
    )
    .join("");
  return `
<section class="kpis">
  <div class="kpi wide">
    <div class="kpi-label">文件</div>
    <div class="kpi-value">${counts.succeeded}<span> / ${total} 成功</span></div>
    <div class="bar">${segments}</div>
    <div class="legends">${legend}</div>
  </div>
  <div class="kpi"><div class="kpi-label">语句</div><div class="kpi-value">${executed}<span> / ${statements}</span></div></div>
  <div class="kpi"><div class="kpi-label">影响行数</div><div class="kpi-value">${rows.toLocaleString()}</div></div>
  <div class="kpi"><div class="kpi-label">总耗时</div><div class="kpi-value">${esc(formatDuration(report.finishedAt - report.startedAt))}</div></div>
</section>`;
}

function failures(report: Report) {
  const failed = report.files.filter(
    (file) => file.outcome === "failed" || (file.outcome === "cancelled" && file.error),
  );
  if (failed.length === 0) return "";
  const cards = failed
    .map((file) => {
      const error = file.error;
      const facts = [
        `步骤 ${file.step}`,
        error ? `第 ${error.index} / ${file.statements} 条语句` : "",
        error?.line
          ? `第 ${error.line} 行`
          : error
            ? `第 ${error.startLine}–${error.endLine} 行`
            : "",
        error?.code ? `错误码 ${error.code}` : "",
      ].filter(Boolean);
      const excerpt = file.excerpt
        ? `<pre class="code">${file.excerpt.lines
            .map((line, index) => {
              const number = file.excerpt!.firstLine + index;
              const [from, to] = file.excerpt!.highlight;
              const marked = number >= from && number <= to;
              return `<span class="ln${marked ? " hit" : ""}"><i>${number}</i>${esc(line) || " "}</span>`;
            })
            .join("")}</pre>`
        : "";
      return `
  <article class="failure ${file.outcome}">
    <header>
      <span class="st sm ${file.outcome}">${outcomeLabels[file.outcome]}</span>
      <code>${esc(file.path)}</code>
    </header>
    <div class="facts">${facts.map((fact) => `<span>${esc(fact)}</span>`).join("")}</div>
    <p class="message">${esc(error?.message ?? file.message ?? "执行失败")}</p>
    ${error?.detail ? `<p class="aside"><b>Detail</b> ${esc(error.detail)}</p>` : ""}
    ${error?.hint ? `<p class="aside"><b>Hint</b> ${esc(error.hint)}</p>` : ""}
    ${excerpt}
  </article>`;
    })
    .join("");
  return `<section><h2>需要处理</h2>${cards}</section>`;
}

function timeline(report: Report) {
  const span = Math.max(1, report.finishedAt - report.startedAt);
  const percent = (ms: number) => ((ms - report.startedAt) / span) * 100;
  const rows = new Map<string, ReportFile[]>();
  for (const file of report.files) {
    const key = `${file.step}:${file.lane}`;
    rows.set(key, [...(rows.get(key) ?? []), file]);
  }
  const lanesPerStep = new Map<number, number>();
  for (const file of report.files) {
    lanesPerStep.set(file.step, Math.max(lanesPerStep.get(file.step) ?? 0, file.lane));
  }
  const body = [...rows.values()]
    .map((files) => {
      const { step, lane } = files[0];
      const label = lanesPerStep.get(step)! > 1 ? `步骤 ${step} · 分支 ${lane}` : `步骤 ${step}`;
      const bars = files
        .filter((file) => file.startedAt !== undefined)
        .map((file) => {
          const left = percent(file.startedAt!);
          const width = Math.max(0.6, percent(file.finishedAt ?? file.startedAt!) - left);
          const tip = `${file.path}\n${outcomeLabels[file.outcome]} · ${duration(file)} · ${file.executed}/${file.statements} 条语句`;
          return `<span class="gbar ${file.outcome}" style="left:${left.toFixed(2)}%;width:${width.toFixed(2)}%" title="${esc(tip)}"><em>${esc(fileName(file.path))}</em></span>`;
        })
        .join("");
      const idle = files.every((file) => file.startedAt === undefined)
        ? `<span class="gidle">${outcomeLabels[files[0].outcome]}</span>`
        : "";
      return `<div class="grow"><div class="glabel">${label}</div><div class="gtrack">${bars}${idle}</div></div>`;
    })
    .join("");
  const ticks = [0, 25, 50, 75, 100]
    .map(
      (tick) =>
        `<span style="left:${tick}%">+${esc(formatDuration(Math.round((span * tick) / 100)))}</span>`,
    )
    .join("");
  return `
<section>
  <h2>时间线</h2>
  <p class="note">每行是一个步骤或并行分支，条形的位置和长度对应文件实际的开始时间和耗时。</p>
  <div class="gantt">${body}<div class="grow axis"><div class="glabel"></div><div class="gticks">${ticks}</div></div></div>
</section>`;
}

function fileList(report: Report) {
  const counts = countOutcomes(report);
  const chips = [
    `<button type="button" class="chip" aria-pressed="true" data-filter="all">全部 ${report.files.length}</button>`,
    ...(Object.keys(outcomeLabels) as FileOutcome[])
      .filter((outcome) => counts[outcome] > 0)
      .map(
        (outcome) =>
          `<button type="button" class="chip" aria-pressed="false" data-filter="${outcome}"><i class="dot ${outcome}"></i>${outcomeLabels[outcome]} ${counts[outcome]}</button>`,
      ),
  ].join("");
  const items = report.files
    .map((file) => {
      const statements = file.log
        .map(
          (entry) => `
        <tr class="${entry.kind}">
          <td>${esc(entry.index ?? "")}</td>
          <td>${entry.kind === "notice" ? "提示" : `${entry.kind === "error" ? "失败 " : ""}${esc(entry.durationMs !== undefined ? formatDuration(entry.durationMs) : "")}`}</td>
          <td>${entry.rows !== undefined ? esc(entry.rows.toLocaleString()) : ""}</td>
          <td><code>${esc(entry.text)}</code></td>
        </tr>`,
        )
        .join("");
      const dropped =
        file.logDropped > 0
          ? `<p class="note">另有 ${file.logDropped} 条较早的日志未保留。</p>`
          : "";
      const detail =
        file.log.length > 0
          ? `<div class="log">${dropped}<table><thead><tr><th>#</th><th>耗时</th><th>行数</th><th>语句</th></tr></thead><tbody>${statements}</tbody></table></div>`
          : `<div class="log"><p class="note">${file.startedAt === undefined ? "这个文件没有执行。" : "没有语句日志。"}</p></div>`;
      return `
    <details class="file" data-outcome="${file.outcome}">
      <summary>
        <span class="c-step">${file.step}${file.lane > 1 || report.files.some((other) => other.step === file.step && other.lane > 1) ? `<small>·${file.lane}</small>` : ""}</span>
        <span class="c-name"><i class="dot ${file.outcome}"></i><code>${esc(file.path)}</code></span>
        <span class="c-status">${outcomeLabels[file.outcome]}</span>
        <span class="c-num">${file.startedAt === undefined ? "—" : `${file.executed}/${file.statements}`}</span>
        <span class="c-num">${file.startedAt === undefined ? "—" : file.rows.toLocaleString()}</span>
        <span class="c-num">${esc(duration(file))}</span>
      </summary>
      ${detail}
    </details>`;
    })
    .join("");
  return `
<section>
  <h2>文件明细</h2>
  <div class="chips">${chips}</div>
  <div class="files">
    <div class="file-head"><span class="c-step">步骤</span><span class="c-name">文件</span><span class="c-status">状态</span><span class="c-num">语句</span><span class="c-num">行数</span><span class="c-num">耗时</span></div>
    ${items}
  </div>
</section>`;
}

function slowest(report: Report) {
  const entries = report.files
    .flatMap((file) =>
      file.log
        .filter((entry) => entry.kind !== "notice" && entry.durationMs !== undefined)
        .map((entry) => ({ file, entry })),
    )
    .sort((a, b) => b.entry.durationMs! - a.entry.durationMs!)
    .slice(0, 8);
  if (entries.length === 0) return "";
  const longest = Math.max(1, entries[0].entry.durationMs!);
  const rows = entries
    .map(
      ({ file, entry }) => `
    <li>
      <div class="slow-top"><code>${esc(fileName(file.path))} #${esc(entry.index ?? "")}</code><b>${esc(formatDuration(entry.durationMs!))}</b></div>
      <div class="slow-bar"><span class="${entry.kind === "error" ? "failed" : "succeeded"}" style="width:${((entry.durationMs! / longest) * 100).toFixed(1)}%"></span></div>
      <code class="slow-sql">${esc(entry.text)}</code>
    </li>`,
    )
    .join("");
  return `<section><h2>最慢的语句</h2><ol class="slow">${rows}</ol></section>`;
}

// Forest tokens, inlined so the report stays a single offline file. Forest
// has only a light theme.
const styles = `
:root{--sidebar-accent:#e4e8e6;--panel:#f8faf9;--card:#fff;--muted:#f4f6f5;--fg:#171a19;--muted-fg:#6b7370;
--placeholder:#9ca3a0;--border:#e4e8e6;--border-strong:#d2d9d6;--primary:#1a4a35;--tech-green:#0fa958;
--primary-tint:rgba(26,74,53,.08);--danger:#dc2626;--danger-ink:#b91c1c;--r-lg:8px;--r-xl:12px;--r-2xl:24px;
--sh-card:0 1px 2px 0 rgba(0,0,0,.06),0 0 1px 0 rgba(0,0,0,.03);
--mono:ui-monospace,"SF Mono",Menlo,Consolas,monospace;color-scheme:light}
.succeeded{--c:var(--tech-green);--tint:var(--primary-tint);--ink:var(--primary)}
.failed{--c:var(--danger);--tint:rgba(220,38,38,.08);--ink:var(--danger-ink)}
.cancelled,.partial{--c:#f97316;--tint:rgba(249,115,22,.11);--ink:#9a3412}
.notRun{--c:var(--placeholder);--tint:var(--muted);--ink:var(--muted-fg)}
.disabled{--c:var(--border-strong);--tint:var(--muted);--ink:var(--muted-fg)}
*{box-sizing:border-box}
body{margin:0;background:var(--panel);color:var(--fg);font:13px/1.5 -apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei","Segoe UI",sans-serif;-webkit-font-smoothing:antialiased}
code,pre{font-family:var(--mono);font-size:12px}
main{max-width:1120px;margin:0 auto;padding:40px 24px 64px;display:flex;flex-direction:column;gap:32px}
.hero{display:flex;flex-direction:column;gap:4px}
.eyebrow{color:var(--muted-fg);font-size:10px;font-weight:600;letter-spacing:.12em}
h1{margin:0;font-size:28px;line-height:34px;font-weight:300;letter-spacing:-.01em;display:flex;flex-wrap:wrap;align-items:center;gap:12px}
h2{margin:0 0 12px;font-size:14px;font-weight:600}
.meta{margin:0;color:var(--muted-fg);font-size:12px}
.meta b{color:var(--fg);font-weight:500}
.st{display:inline-flex;align-items:center;gap:6px;height:22px;padding:0 9px 0 8px;border-radius:9999px;background:var(--tint);color:var(--ink);font-size:12px;font-weight:500;letter-spacing:0;white-space:nowrap}
.st::before{content:"";flex:none;width:6px;height:6px;border-radius:9999px;background:var(--c)}
.st.sm{height:20px;padding:0 8px 0 7px;font-size:11px}
section{display:flex;flex-direction:column}
.note{margin:-6px 0 12px;color:var(--muted-fg);font-size:12px}
.kpi,.failure,.gantt,.files,.slow li{background:var(--card);border:1px solid var(--border);border-radius:var(--r-xl);box-shadow:var(--sh-card)}
.kpis{display:grid;grid-template-columns:2fr repeat(3,minmax(0,1fr));gap:12px}
.kpi{padding:14px 16px;display:flex;flex-direction:column;gap:4px;min-width:0}
.kpi-label{color:var(--muted-fg);font-size:12px}
.kpi-value{font-family:var(--mono);font-variant-numeric:tabular-nums;font-size:22px;font-weight:300;line-height:1.25;letter-spacing:-.01em;white-space:nowrap}
.kpi-value span{font-size:13px;color:var(--muted-fg)}
.bar{display:flex;height:6px;margin-top:6px;border-radius:9999px;overflow:hidden;gap:2px;background:var(--sidebar-accent)}
.seg{min-width:4px;background:var(--c)}
.dot{display:inline-block;flex:none;width:6px;height:6px;margin-right:6px;border-radius:9999px;background:var(--c)}
.legends{display:flex;flex-wrap:wrap;gap:4px 14px;font-size:12px;color:var(--muted-fg)}
.legend b{font-family:var(--mono);color:var(--fg);font-weight:500}
.failure{padding:16px 18px;display:flex;flex-direction:column;gap:8px;margin-bottom:12px}
.failure header{display:flex;align-items:center;gap:10px;flex-wrap:wrap}
.failure header code{font-size:13px;font-weight:500}
.facts{display:flex;flex-wrap:wrap;gap:6px}
.facts span{display:inline-flex;align-items:center;height:20px;padding:0 7px;border-radius:6px;background:var(--muted);color:var(--muted-fg);font-size:11px;font-weight:500}
.message{margin:0;font-family:var(--mono);font-size:12px;color:var(--ink);white-space:pre-wrap;word-break:break-word}
.aside{margin:0;color:var(--muted-fg);font-size:12px}
.aside b{color:var(--fg);font-weight:600;margin-right:6px}
.code{margin:4px 0 0;background:var(--panel);border:1px solid var(--border);border-radius:var(--r-lg);padding:8px 0;overflow-x:auto}
.ln{display:block;padding:0 14px 0 0;line-height:20px;white-space:pre}
.ln i{display:inline-block;width:44px;padding-right:12px;text-align:right;color:var(--placeholder);font-style:normal;user-select:none}
.ln.hit{background:rgba(220,38,38,.08)}
.ln.hit i{color:var(--danger-ink);font-weight:600}
.gantt{padding:14px 18px 10px;overflow-x:auto}
.grow{display:grid;grid-template-columns:120px minmax(480px,1fr);align-items:center;min-height:30px}
.glabel{color:var(--muted-fg);font-size:12px;white-space:nowrap}
.gtrack{position:relative;height:22px;border-left:1px solid var(--border)}
.grow:not(.axis) .gtrack{background:repeating-linear-gradient(90deg,transparent 0 calc(25% - 1px),var(--muted) calc(25% - 1px) 25%)}
.gbar{position:absolute;top:3px;height:16px;padding:0 6px;border-radius:5px;overflow:hidden;background:color-mix(in srgb,var(--c) 16%,var(--card));box-shadow:inset 0 0 0 1px color-mix(in srgb,var(--c) 55%,var(--card));color:var(--ink);font-size:11px;line-height:16px}
.gbar em{font-family:var(--mono);font-style:normal;white-space:nowrap}
.gidle{font-size:12px;color:var(--muted-fg);padding-left:10px;line-height:22px}
.axis .gtrack,.gticks{height:22px;position:relative;border:0}
.gticks span{position:absolute;top:4px;transform:translateX(-50%);font-family:var(--mono);font-size:11px;color:var(--muted-fg);white-space:nowrap}
.gticks span:first-child{transform:none}
.gticks span:last-child{transform:translateX(-100%)}
.chips{display:flex;flex-wrap:wrap;gap:8px;margin-bottom:12px}
.chip{display:inline-flex;align-items:center;height:32px;padding:0 12px;border:1px solid rgba(107,115,112,.3);border-radius:var(--r-2xl);background:var(--card);color:var(--fg);font:inherit;font-size:12px;cursor:pointer;transition:background-color .2s}
.chip:hover{background:var(--muted)}
.chip[aria-pressed=true]{background:var(--primary-tint);border-color:rgba(26,74,53,.35);color:var(--primary)}
.chip:focus-visible,.file summary:focus-visible{outline:2px solid var(--primary);outline-offset:-2px}
.files{overflow:hidden}
.file-head,.file summary{display:grid;grid-template-columns:56px minmax(0,1fr) 72px 72px 80px 84px;gap:12px;align-items:center;padding:9px 18px}
.file-head{min-height:36px;padding-top:0;padding-bottom:0;background:var(--panel);color:var(--muted-fg);font-size:12px;font-weight:500;border-bottom:1px solid var(--border)}
.file{border-bottom:1px solid rgba(228,232,230,.7)}
.file:last-child{border-bottom:0}
.file summary{cursor:pointer;list-style:none;transition:background-color .15s}
.file summary::-webkit-details-marker{display:none}
.file summary:hover{background:rgba(244,246,245,.8)}
.file[open] summary{background:rgba(26,74,53,.05)}
.c-step,.c-num{font-family:var(--mono);font-variant-numeric:tabular-nums;font-size:12px}
.c-step small{color:var(--muted-fg);margin-left:1px}
.c-name{display:flex;align-items:center;min-width:0}
.c-name code{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.c-num{text-align:right}
.file-head .c-step,.file-head .c-num{font-family:inherit}
.file[data-outcome=failed] .c-status{color:var(--danger-ink);font-weight:500}
.file[data-outcome=cancelled] .c-status{color:#9a3412;font-weight:500}
.file[data-outcome=notRun] .c-status,.file[data-outcome=disabled] .c-status{color:var(--muted-fg)}
.log{padding:4px 18px 16px 86px;overflow-x:auto}
.log .note{margin:8px 0}
.log table{width:100%;border-collapse:collapse;font-size:12px}
.log th{white-space:nowrap;text-align:left;color:var(--muted-fg);font-weight:500;padding:6px 8px;border-bottom:1px solid var(--border)}
.log td{padding:5px 8px;border-bottom:1px solid var(--muted);vertical-align:top;white-space:nowrap;font-variant-numeric:tabular-nums}
.log td:last-child{white-space:normal;word-break:break-word;width:100%}
.log tr.error td{color:var(--danger-ink)}
.log tr.notice td{color:var(--muted-fg)}
.slow{list-style:none;margin:0;padding:0;display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}
.slow li{padding:12px 16px;display:flex;flex-direction:column;gap:6px;min-width:0}
.slow-top{display:flex;justify-content:space-between;gap:10px}
.slow-top b{font-family:var(--mono);font-weight:500;font-variant-numeric:tabular-nums}
.slow-bar{height:6px;background:var(--sidebar-accent);border-radius:9999px;overflow:hidden}
.slow-bar span{display:block;height:100%;border-radius:inherit;background:var(--c)}
.slow-sql{color:var(--muted-fg);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
footer{color:var(--muted-fg);font-size:12px;border-top:1px solid var(--border);padding-top:16px}
@media (max-width:760px){.kpis{grid-template-columns:repeat(2,minmax(0,1fr))}.kpi.wide{grid-column:1/-1}
.slow{grid-template-columns:1fr}.file-head{display:none}
.file summary{grid-template-columns:40px minmax(0,1fr) auto;gap:6px 10px}.file summary .c-num{display:none}
.log{padding-left:18px}}
@media print{body{background:#fff}.chips{display:none}.file summary:hover{background:none}
.kpi,.failure,.gantt,.files,.slow li{box-shadow:none}}
`;

const script = `
document.querySelectorAll(".chip").forEach(function (chip) {
  chip.addEventListener("click", function () {
    var filter = chip.getAttribute("data-filter");
    document.querySelectorAll(".chip").forEach(function (other) { other.setAttribute("aria-pressed", String(other === chip)); });
    document.querySelectorAll(".file").forEach(function (file) {
      file.hidden = filter !== "all" && file.getAttribute("data-outcome") !== filter;
    });
  });
});
`;

export function toHtml(report: Report) {
  // The server reports the version with the driver label in front.
  const engine = report.version?.startsWith(report.driver)
    ? report.version
    : [report.driver, report.version].filter(Boolean).join(" ");
  return `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>执行报告 · ${esc(report.connection)}</title>
<style>${styles}</style>
</head>
<body>
<main>
<header class="hero">
  <div class="eyebrow">SchemaPilot 执行报告</div>
  <h1>${esc(report.connection)} <span class="st ${report.outcome}">${runLabels[report.outcome]}</span></h1>
  <p class="meta"><b>${esc(engine)}</b> · 开始 ${clock(report.startedAt)} · 结束 ${clock(report.finishedAt)} · ${report.steps} 个步骤</p>
</header>
${kpis(report)}
${failures(report)}
${timeline(report)}
${fileList(report)}
${slowest(report)}
<footer>生成于 ${clock(report.generatedAt)}。每个文件显示最后一次执行的结果；日志中的 SQL 最多保留 200 个字符。</footer>
</main>
<script>${script}</script>
</body>
</html>
`;
}

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
      <span class="pill ${file.outcome}">${outcomeLabels[file.outcome]}</span>
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
    `<button type="button" class="chip on" data-filter="all">全部 ${report.files.length}</button>`,
    ...(Object.keys(outcomeLabels) as FileOutcome[])
      .filter((outcome) => counts[outcome] > 0)
      .map(
        (outcome) =>
          `<button type="button" class="chip" data-filter="${outcome}"><i class="dot ${outcome}"></i>${outcomeLabels[outcome]} ${counts[outcome]}</button>`,
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

const styles = `
:root{--bg:#f5f6f8;--surface:#fff;--text:#14181f;--muted:#5d6675;--line:#e2e5ea;--soft:#eef0f3;
--succeeded:#1d8a4b;--failed:#c9372c;--cancelled:#b5761a;--notRun:#9aa2ae;--disabled:#c4c9d1;--accent:#2f6feb;
--succeeded-bg:#e6f4ec;--failed-bg:#fbeceb;--cancelled-bg:#fbf1e1;color-scheme:light}
@media (prefers-color-scheme:dark){:root{--bg:#0f1216;--surface:#171b21;--text:#e7eaef;--muted:#9aa3b0;--line:#2a3038;--soft:#1e232a;
--succeeded:#3fb46e;--failed:#f0645b;--cancelled:#e2a541;--notRun:#6d7581;--disabled:#4a515b;--accent:#5b8ff9;
--succeeded-bg:#16291f;--failed-bg:#33191a;--cancelled-bg:#30261a;color-scheme:dark}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--text);font:14px/1.55 -apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei","Segoe UI",sans-serif;-webkit-font-smoothing:antialiased}
code,pre{font-family:ui-monospace,"SF Mono",Menlo,Consolas,monospace;font-size:12.5px}
main{max-width:1120px;margin:0 auto;padding:40px 24px 64px;display:flex;flex-direction:column;gap:28px}
h1{margin:4px 0 6px;font-size:28px;line-height:1.2;display:flex;flex-wrap:wrap;align-items:center;gap:12px}
h2{margin:0 0 12px;font-size:16px}
.eyebrow{color:var(--muted);font-size:13px;letter-spacing:.02em}
.meta{margin:0;color:var(--muted)}
.meta b{color:var(--text);font-weight:500}
.status{font-size:13px;font-weight:600;padding:4px 10px;border-radius:999px}
.status.succeeded{background:var(--succeeded-bg);color:var(--succeeded)}
.status.failed{background:var(--failed-bg);color:var(--failed)}
.status.cancelled,.status.partial{background:var(--cancelled-bg);color:var(--cancelled)}
section{display:flex;flex-direction:column}
.note{margin:-6px 0 12px;color:var(--muted);font-size:13px}
.kpis{display:grid;grid-template-columns:2fr repeat(3,minmax(0,1fr));gap:12px}
.kpi{background:var(--surface);border:1px solid var(--line);border-radius:14px;padding:16px 18px;display:flex;flex-direction:column;gap:6px;min-width:0}
.kpi-label{color:var(--muted);font-size:13px}
.kpi-value{font-size:26px;font-weight:650;font-variant-numeric:tabular-nums;white-space:nowrap}
.kpi-value span{font-size:15px;font-weight:500;color:var(--muted)}
.bar{display:flex;height:8px;border-radius:99px;overflow:hidden;gap:2px;margin-top:4px}
.seg{min-width:4px}
.seg.succeeded,.dot.succeeded,.gbar.succeeded,.slow-bar .succeeded{background:var(--succeeded)}
.seg.failed,.dot.failed,.gbar.failed,.slow-bar .failed{background:var(--failed)}
.seg.cancelled,.dot.cancelled,.gbar.cancelled{background:var(--cancelled)}
.seg.notRun,.dot.notRun{background:var(--notRun)}
.seg.disabled,.dot.disabled{background:var(--disabled)}
.legends{display:flex;flex-wrap:wrap;gap:4px 14px;font-size:12.5px;color:var(--muted)}
.legend b{color:var(--text);font-weight:600}
.dot{display:inline-block;width:8px;height:8px;border-radius:50%;margin-right:6px;flex:none}
.failure{background:var(--surface);border:1px solid var(--line);border-radius:14px;padding:16px 18px;display:flex;flex-direction:column;gap:8px;margin-bottom:12px}
.failure.failed{border-color:color-mix(in srgb,var(--failed) 45%,var(--line))}
.failure header{display:flex;align-items:center;gap:10px;flex-wrap:wrap}
.failure header code{font-size:14px;font-weight:600}
.pill{font-size:12px;font-weight:600;padding:2px 8px;border-radius:6px}
.pill.failed{background:var(--failed-bg);color:var(--failed)}
.pill.cancelled{background:var(--cancelled-bg);color:var(--cancelled)}
.facts{display:flex;flex-wrap:wrap;gap:6px}
.facts span{background:var(--soft);border-radius:6px;padding:2px 8px;font-size:12.5px;color:var(--muted)}
.message{margin:0;font-family:ui-monospace,"SF Mono",Menlo,Consolas,monospace;font-size:13px;white-space:pre-wrap;word-break:break-word}
.aside{margin:0;color:var(--muted);font-size:13px}
.aside b{color:var(--text);font-weight:600;margin-right:6px}
.code{margin:4px 0 0;background:var(--soft);border-radius:10px;padding:10px 0;overflow-x:auto}
.ln{display:block;padding:0 14px 0 0;white-space:pre}
.ln i{display:inline-block;width:44px;padding-right:12px;text-align:right;color:var(--muted);font-style:normal;user-select:none}
.ln.hit{background:color-mix(in srgb,var(--failed) 14%,transparent)}
.ln.hit i{color:var(--failed);font-weight:600}
.gantt{background:var(--surface);border:1px solid var(--line);border-radius:14px;padding:14px 18px 10px;overflow-x:auto}
.grow{display:grid;grid-template-columns:120px minmax(480px,1fr);align-items:center;min-height:30px}
.glabel{color:var(--muted);font-size:12.5px;white-space:nowrap}
.gtrack{position:relative;height:22px;border-left:1px solid var(--line)}
.grow:not(.axis) .gtrack{background:repeating-linear-gradient(90deg,transparent 0 calc(25% - 1px),var(--soft) calc(25% - 1px) 25%)}
.gbar{position:absolute;top:3px;height:16px;border-radius:5px;overflow:hidden;color:#fff;font-size:11px;line-height:16px;padding:0 6px}
.gbar em{font-style:normal;white-space:nowrap}
.gidle{font-size:12px;color:var(--muted);padding-left:10px;line-height:22px}
.axis .gtrack,.gticks{height:22px;position:relative;border:0}
.gticks span{position:absolute;top:4px;transform:translateX(-50%);font-size:11.5px;color:var(--muted);white-space:nowrap}
.gticks span:first-child{transform:none}
.gticks span:last-child{transform:translateX(-100%)}
.chips{display:flex;flex-wrap:wrap;gap:8px;margin-bottom:12px}
.chip{font:inherit;font-size:13px;display:inline-flex;align-items:center;border:1px solid var(--line);background:var(--surface);color:var(--text);border-radius:999px;padding:5px 12px;cursor:pointer;min-height:32px}
.chip.on{border-color:var(--accent);color:var(--accent);background:color-mix(in srgb,var(--accent) 8%,var(--surface))}
.files{background:var(--surface);border:1px solid var(--line);border-radius:14px;overflow:hidden}
.file-head,.file summary{display:grid;grid-template-columns:56px minmax(0,1fr) 72px 72px 80px 84px;gap:12px;align-items:center;padding:10px 18px}
.file-head{color:var(--muted);font-size:12.5px;border-bottom:1px solid var(--line)}
.file{border-bottom:1px solid var(--line)}
.file:last-child{border-bottom:0}
.file summary{cursor:pointer;list-style:none}
.file summary::-webkit-details-marker{display:none}
.file summary:hover{background:var(--soft)}
.file[open] summary{background:var(--soft)}
.c-step small{color:var(--muted);margin-left:1px}
.c-name{display:flex;align-items:center;min-width:0}
.c-name code{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.c-num{text-align:right;font-variant-numeric:tabular-nums}
.file-head .c-num{text-align:right}
.file[data-outcome=failed] .c-status{color:var(--failed);font-weight:600}
.file[data-outcome=cancelled] .c-status{color:var(--cancelled);font-weight:600}
.file[data-outcome=notRun] .c-status,.file[data-outcome=disabled] .c-status{color:var(--muted)}
.log{padding:4px 18px 16px 86px;overflow-x:auto}
.log .note{margin:8px 0}
.log table{width:100%;border-collapse:collapse;font-size:12.5px}
.log th{white-space:nowrap;text-align:left;color:var(--muted);font-weight:500;padding:6px 8px;border-bottom:1px solid var(--line)}
.log td{padding:5px 8px;border-bottom:1px solid var(--soft);vertical-align:top;white-space:nowrap}
.log td:last-child{white-space:normal;word-break:break-word;width:100%}
.log tr.error td{color:var(--failed)}
.log tr.notice td{color:var(--muted)}
.slow{list-style:none;margin:0;padding:0;display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:10px}
.slow li{background:var(--surface);border:1px solid var(--line);border-radius:12px;padding:12px 14px;display:flex;flex-direction:column;gap:6px;min-width:0}
.slow-top{display:flex;justify-content:space-between;gap:10px}
.slow-top b{font-variant-numeric:tabular-nums}
.slow-bar{height:4px;background:var(--soft);border-radius:99px;overflow:hidden}
.slow-bar span{display:block;height:100%}
.slow-sql{color:var(--muted);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
footer{color:var(--muted);font-size:12.5px;border-top:1px solid var(--line);padding-top:16px}
@media (max-width:760px){.kpis{grid-template-columns:repeat(2,minmax(0,1fr))}.kpi.wide{grid-column:1/-1}
.slow{grid-template-columns:1fr}.file-head{display:none}
.file summary{grid-template-columns:40px minmax(0,1fr) auto;gap:6px 10px}.file summary .c-num{display:none}
.log{padding-left:18px}}
@media print{body{background:#fff}.chips{display:none}.file summary:hover{background:none}}
`;

const script = `
document.querySelectorAll(".chip").forEach(function (chip) {
  chip.addEventListener("click", function () {
    var filter = chip.getAttribute("data-filter");
    document.querySelectorAll(".chip").forEach(function (other) { other.classList.toggle("on", other === chip); });
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
<header>
  <div class="eyebrow">SchemaPilot 执行报告</div>
  <h1>${esc(report.connection)} <span class="status ${report.outcome}">${runLabels[report.outcome]}</span></h1>
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

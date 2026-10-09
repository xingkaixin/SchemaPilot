/** Files of a lane run in order; lanes of a step run in parallel; steps run in order. */
export type Lane = string[];
export type Step = Lane[];

export type DropTarget =
  | { kind: "gap"; index: number }
  | { kind: "step"; step: number }
  | { kind: "lane"; step: number; lane: number; position: number };

export function comparePaths(a: string, b: string) {
  return a.localeCompare(b, undefined, { numeric: true });
}

const branchName = /^(\d+)_(\d+)_/;

/**
 * Orders files by name, one step each. Files named {step}_{branch}_*.sql
 * that share a directory and step number form one step instead: a lane per
 * branch number, each lane running its files in name order.
 */
export function arrangeByName(paths: string[]): Step[] {
  const order: (string | Map<number, Lane>)[] = [];
  const groups = new Map<string, Map<number, Lane>>();
  for (const path of [...paths].sort(comparePaths)) {
    const slash = path.lastIndexOf("/") + 1;
    const match = branchName.exec(path.slice(slash));
    if (!match) {
      order.push(path);
      continue;
    }
    const key = path.slice(0, slash) + Number(match[1]);
    let lanes = groups.get(key);
    if (!lanes) {
      lanes = new Map();
      groups.set(key, lanes);
      order.push(lanes);
    }
    const branch = Number(match[2]);
    lanes.set(branch, [...(lanes.get(branch) ?? []), path]);
  }
  return order.map((item) =>
    typeof item === "string"
      ? [[item]]
      : [...item.entries()].sort(([a], [b]) => a - b).map(([, lane]) => lane),
  );
}

export function allPaths(steps: Step[]): string[] {
  return steps.flat(2);
}

export function locate(steps: Step[], path: string) {
  for (let step = 0; step < steps.length; step++) {
    for (let lane = 0; lane < steps[step].length; lane++) {
      const position = steps[step][lane].indexOf(path);
      if (position >= 0) return { step, lane, position };
    }
  }
  return null;
}

function clone(steps: Step[]): Step[] {
  return steps.map((step) => step.map((lane) => [...lane]));
}

// Mutates in place so that references to surviving lanes and steps held by
// the caller stay valid.
function detach(steps: Step[], path: string) {
  const found = locate(steps, path);
  if (!found) return;
  const step = steps[found.step];
  step[found.lane].splice(found.position, 1);
  if (step[found.lane].length === 0) step.splice(found.lane, 1);
  if (step.length === 0) steps.splice(found.step, 1);
}

export function removeFiles(steps: Step[], paths: string[]): Step[] {
  const next = clone(steps);
  for (const path of paths) detach(next, path);
  return next;
}

export function moveFile(steps: Step[], path: string, target: DropTarget): Step[] {
  const next = clone(steps);
  if (target.kind === "gap") {
    const following = next.slice(target.index);
    detach(next, path);
    const anchor = following.find((step) => next.includes(step));
    next.splice(anchor ? next.indexOf(anchor) : next.length, 0, [[path]]);
    return next;
  }
  const step = next[target.step];
  if (!step) return steps;
  if (target.kind === "step") {
    detach(next, path);
    if (!next.includes(step)) return steps;
    step.push([path]);
    return next;
  }
  const lane = step[target.lane];
  if (!lane) return steps;
  const following = lane.slice(target.position).filter((item) => item !== path);
  detach(next, path);
  if (!step.includes(lane)) {
    if (!next.includes(step)) return steps;
    step.push([path]);
    return next;
  }
  const anchor = following.find((item) => lane.includes(item));
  lane.splice(anchor ? lane.indexOf(anchor) : lane.length, 0, path);
  return next;
}

function alone(steps: Step[], path: string) {
  const found = locate(steps, path);
  return found !== null && allPaths([steps[found.step]]).length === 1;
}

export function parallelWithPrevious(steps: Step[], path: string): Step[] {
  const found = locate(steps, path);
  if (!found || found.step === 0) return steps;
  return moveFile(steps, path, { kind: "step", step: found.step - 1 });
}

export function extractToOwnStep(steps: Step[], path: string): Step[] {
  const found = locate(steps, path);
  if (!found || alone(steps, path)) return steps;
  return moveFile(steps, path, { kind: "gap", index: found.step + 1 });
}

export function shift(steps: Step[], path: string, direction: -1 | 1): Step[] {
  const found = locate(steps, path);
  if (!found) return steps;
  if (!alone(steps, path)) {
    return moveFile(steps, path, { kind: "gap", index: found.step + (direction === 1 ? 1 : 0) });
  }
  const swapWith = found.step + direction;
  if (swapWith < 0 || swapWith >= steps.length) return steps;
  const next = clone(steps);
  [next[found.step], next[swapWith]] = [next[swapWith], next[found.step]];
  return next;
}

/** Paths that come after the given file in execution order, including parallel lanes of later steps. */
export function pathsAfter(steps: Step[], path: string): string[] {
  const found = locate(steps, path);
  if (!found) return [];
  const sameLane = steps[found.step][found.lane].slice(found.position + 1);
  return [...sameLane, ...allPaths(steps.slice(found.step + 1))];
}

/** A step holding one lane of several files is the same as consecutive steps; keep the simpler shape. */
export function normalize(steps: Step[]): Step[] {
  return steps.flatMap((step) =>
    step.length === 1 && step[0].length > 1 ? step[0].map((path) => [[path]]) : [step],
  );
}

// diff-tour's guard for Pi, loaded with -e. Pi has no permissions of its own, so this extension refuses every tool
// call that is not read, grep, find or ls, and every path that leads outside the working directory (the export
// directory tour starts Pi in). It fails closed: anything it cannot check is refused.
import { realpathSync } from "node:fs";
import { homedir } from "node:os";
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";

const allowed = new Set(["read", "grep", "find", "ls"]);

// Pi's own path resolution (resolveToCwd): Unicode spaces become spaces, a leading @ is dropped, ~ is the home
// directory, and file:// URLs are paths.
function toPath(input: string, cwd: string): string {
	let p = input.replace(/[  -   　]/g, " ");
	if (p.startsWith("@")) p = p.slice(1);
	if (p === "~") p = homedir();
	else if (p.startsWith("~/") || p.startsWith("~\\")) p = join(homedir(), p.slice(2));
	else if (/^file:\/\//.test(p)) p = fileURLToPath(p);
	return resolve(cwd, p);
}

// real follows symlinks in the part of p that exists.
function real(p: string): string {
	const rest: string[] = [];
	for (let cur = p; ; ) {
		try {
			return join(realpathSync(cur), ...rest.reverse());
		} catch {
			const parent = dirname(cur);
			if (parent === cur) return p;
			rest.push(basename(cur));
			cur = parent;
		}
	}
}

function inside(root: string, p: string): boolean {
	const r = relative(root, p);
	return r === "" || (r !== ".." && !r.startsWith(".." + sep) && !isAbsolute(r));
}

export default function (pi: any) {
	pi.on("tool_call", (event: any, ctx: any) => {
		try {
			if (!allowed.has(event.toolName)) {
				return { block: true, reason: `diff-tour allows only read, grep, find and ls, not ${event.toolName}` };
			}
			const root = realpathSync(ctx.cwd);
			const path = event.input?.path;
			if (path !== undefined && typeof path !== "string") {
				return { block: true, reason: "diff-tour could not check this path" };
			}
			if (!inside(root, real(toPath(path || ".", ctx.cwd)))) {
				return { block: true, reason: "diff-tour allows reading only inside the working directory" };
			}
			return undefined;
		} catch {
			return { block: true, reason: "diff-tour could not check this tool call" };
		}
	});
}

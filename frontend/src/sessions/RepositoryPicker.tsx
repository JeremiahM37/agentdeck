import { useEffect, useRef, useState } from "react";
import type { Project } from "../types";
export interface RepositorySelection {
  project_id: number;
  base: string;
}
export function RepositoryPicker({
  projects,
  primaryID,
  value,
  onChange,
}: {
  projects: Project[];
  primaryID: number | null;
  value: RepositorySelection[];
  onChange: (value: RepositorySelection[]) => void;
}) {
  const primary = projects.find((project) => project.id === primaryID),
    available = projects.filter(
      (project) =>
        primary &&
        project.target_id === primary.target_id &&
        project.id !== primary.id &&
        !value.some((row) => row.project_id === project.id),
    );
  const [selected, setSelected] = useState(""),
    picker = useRef<HTMLSelectElement>(null),
    rows = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const valid = value.filter((row) => {
      const project = projects.find((project) => project.id === row.project_id);
      return (
        primary &&
        project &&
        project.id !== primary.id &&
        project.target_id === primary.target_id
      );
    });
    if (valid.length !== value.length) onChange(valid);
  }, [primaryID, projects]);
  return (
    <details>
      <summary>
        Additional repositories{value.length ? ` (${value.length})` : ""}
      </summary>
      <p className="subhint">
        Add up to seven other projects on the same target. Each gets separate
        files on the new branch.
      </p>
      <select
        className="f"
        ref={picker}
        aria-label="Additional repository"
        value={
          available.some((project) => String(project.id) === selected)
            ? selected
            : String(available[0]?.id || "")
        }
        disabled={!available.length || value.length >= 7}
        onChange={(event) => setSelected(event.target.value)}
      >
        {available.length ? (
          available.map((project) => (
            <option key={project.id} value={project.id}>
              {project.name}
            </option>
          ))
        ) : (
          <option value="">No other projects on this target</option>
        )}
      </select>
      <button
        className="b"
        type="button"
        disabled={!available.length || value.length >= 7}
        onClick={() => {
          const id = Number(picker.current?.value);
          if (!available.some((project) => project.id === id)) return;
          onChange([...value, { project_id: id, base: "" }]);
          requestAnimationFrame(() =>
            rows.current?.lastElementChild?.querySelector("input")?.focus(),
          );
        }}
      >
        Add repository
      </button>
      <div ref={rows} aria-label="Selected repositories">
        {value.map((row) => {
          const project = projects.find(
            (project) => project.id === row.project_id,
          );
          if (!project) return null;
          return (
            <div key={row.project_id} className="workspace-repository">
              <strong>{project.name}</strong>
              <label className="f">
                Base for {project.name}
                <input
                  className="f"
                  aria-label={`Base for ${project.name}`}
                  placeholder="HEAD — current committed revision"
                  value={row.base}
                  onChange={(event) =>
                    onChange(
                      value.map((item) =>
                        item.project_id === row.project_id
                          ? { ...item, base: event.target.value }
                          : item,
                      ),
                    )
                  }
                />
              </label>
              <button
                className="b"
                type="button"
                aria-label={`Remove ${project.name}`}
                onClick={() => {
                  onChange(
                    value.filter((item) => item.project_id !== row.project_id),
                  );
                  picker.current?.focus();
                }}
              >
                Remove
              </button>
            </div>
          );
        })}
      </div>
    </details>
  );
}

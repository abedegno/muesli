import React, { useCallback, useEffect, useState } from "react";
import type { ApiClient } from "../api/client";
import type { Plugin, PluginHealth, PluginInput, PluginPatch } from "../api/types";
import { schemaToFields, type FormField } from "../schema/schemaForm";

interface Props {
  client: Pick<
    ApiClient,
    "listPlugins" | "createPlugin" | "updatePlugin" | "deletePlugin" | "checkPluginHealth"
  >;
}

function StatusPill({ on, labelOn, labelOff }: { on: boolean; labelOn: string; labelOff: string }) {
  return (
    <span className={`pill ${on ? "pill-yes" : "pill-no"}`}>
      {on ? `✓ ${labelOn}` : `✗ ${labelOff}`}
    </span>
  );
}

export function PluginsView({ client }: Props) {
  const [plugins, setPlugins] = useState<Plugin[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setPlugins(await client.listPlugins());
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "failed to load plugins");
    }
  }, [client]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  async function act(fn: () => Promise<unknown>) {
    try {
      await fn();
      await refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "operation failed");
    }
  }

  return (
    <section>
      <div className="row">
        <h1>Plugins</h1>
        <button onClick={() => setAdding((v) => !v)}>{adding ? "Cancel" : "Add plugin"}</button>
      </div>
      {error && <p className="error">{error}</p>}

      {adding && (
        <PluginEditor
          onSave={(input) => act(() => client.createPlugin(input)).then(() => setAdding(false))}
        />
      )}

      <table>
        <thead>
          <tr>
            <th>Name</th>
            <th>Kind</th>
            <th>Endpoint</th>
            <th>Enabled</th>
            <th>Default</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          {plugins.map((p) => (
            <React.Fragment key={p.id}>
              <tr>
                <td>{p.name}</td>
                <td>
                  <span className={`badge badge-${p.kind}`}>{p.kind}</span>
                </td>
                <td className="endpoint-url">{p.endpoint_url}</td>
                <td>
                  <StatusPill on={p.enabled} labelOn="Enabled" labelOff="Disabled" />
                </td>
                <td>
                  <StatusPill on={p.is_default} labelOn="Default" labelOff="No" />
                </td>
                <td className="row">
                  <button onClick={() => act(() => client.updatePlugin(p.id, { enabled: !p.enabled }))}>
                    {p.enabled ? "Disable" : "Enable"}
                  </button>
                  {!p.is_default && (
                    <button onClick={() => act(() => client.updatePlugin(p.id, { is_default: true }))}>
                      Set default
                    </button>
                  )}
                  <button onClick={() => act(() => client.deletePlugin(p.id))}>Delete</button>
                </td>
              </tr>
              <PluginConfigRow plugin={p} client={client} onSaved={refresh} />
            </React.Fragment>
          ))}
        </tbody>
      </table>
    </section>
  );
}

/**
 * PluginConfigRow renders a per-plugin "Configure" toggle in its own table
 * row, and (when open) the config editor spanning the table's columns.
 * Plugins with no config_schema (or an empty one) render nothing at all —
 * same as today's no-op.
 */
function PluginConfigRow({
  plugin,
  client,
  onSaved,
}: {
  plugin: Plugin;
  client: Props["client"];
  onSaved: () => void;
}) {
  const [open, setOpen] = useState(false);
  const fields = schemaToFields(plugin.config_schema);
  if (fields.length === 0) return null;

  return (
    <tr>
      <td colSpan={6}>
        <button onClick={() => setOpen((v) => !v)}>{open ? "Close" : "Configure"}</button>
        {open && (
          <PluginConfigEditor plugin={plugin} fields={fields} client={client} onSaved={onSaved} />
        )}
      </td>
    </tr>
  );
}

/** initialConfigValue picks the starting value for one field: the plugin's
 * current config value, falling back to the schema default, then (for enum
 * fields) the first option. Secret fields are always seeded empty — an empty
 * secret input means "leave unchanged", matching PluginEditor's convention;
 * the server's redacted "*" placeholder must never be shown as a real value. */
function initialConfigValue(field: FormField, config: Record<string, unknown> | undefined): unknown {
  if (field.control === "secret") return "";
  const current = config?.[field.name];
  if (current !== undefined) return current;
  if (field.default !== undefined) return field.default;
  if (field.control === "select" && field.options && field.options.length > 0) return field.options[0];
  if (field.control === "checkbox") return false;
  return "";
}

function initialConfigValues(fields: FormField[], config: Record<string, unknown> | undefined) {
  const out: Record<string, unknown> = {};
  for (const field of fields) {
    out[field.name] = initialConfigValue(field, config);
  }
  return out;
}

/**
 * PluginConfigEditor is the schema-driven config editor for one plugin. When
 * the schema has one or more enum-typed fields (the TR06-precedent shape,
 * e.g. model/compute_type) it renders a dedicated picker block of native
 * <select>s above the generic form for the remaining fields; otherwise it
 * renders only the generic form. Apply saves the config via updatePlugin and
 * then immediately checks plugin health, rendering the result inline.
 */
function PluginConfigEditor({
  plugin,
  fields,
  client,
  onSaved,
}: {
  plugin: Plugin;
  fields: FormField[];
  client: Props["client"];
  onSaved: () => void;
}) {
  const enumFields = fields.filter((f) => f.control === "select");
  const otherFields = fields.filter((f) => f.control !== "select");

  const [values, setValues] = useState<Record<string, unknown>>(() =>
    initialConfigValues(fields, plugin.config)
  );
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [health, setHealth] = useState<PluginHealth | null>(null);

  function setField(name: string, value: unknown) {
    setHealth(null);
    setValues((v) => ({ ...v, [name]: value }));
  }

  function buildConfig(): Record<string, unknown> {
    const config: Record<string, unknown> = {};
    for (const field of fields) {
      const value = values[field.name];
      // A blank secret means "leave unchanged" — never send it, so the
      // server keeps the stored value instead of overwriting it with "".
      if (field.control === "secret" && (value === undefined || value === "")) continue;
      config[field.name] = value;
    }
    return config;
  }

  async function apply() {
    setSaving(true);
    setSaveError(null);
    setHealth(null);
    const patch: PluginPatch = { config: buildConfig() };
    try {
      await client.updatePlugin(plugin.id, patch);
    } catch (err) {
      setSaving(false);
      setSaveError(err instanceof Error ? err.message : "failed to save config");
      return;
    }
    onSaved();
    try {
      setHealth(await client.checkPluginHealth(plugin.id));
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : "health check failed");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="plugin-config">
      {enumFields.length > 0 && (
        <div className="plugin-config-picker">
          {enumFields.map((field) => {
            const inputId = `config-${plugin.id}-${field.name}`;
            return (
              <p key={field.name}>
                <label htmlFor={inputId}>{field.label}</label>
                <select
                  id={inputId}
                  value={String(values[field.name] ?? "")}
                  onChange={(e) => setField(field.name, e.target.value)}
                >
                  {(field.options ?? []).map((opt) => (
                    <option key={opt} value={opt}>
                      {opt}
                    </option>
                  ))}
                </select>
              </p>
            );
          })}
        </div>
      )}

      {otherFields.length > 0 && (
        <div className="plugin-config-form">
          {otherFields.map((field) => (
            <ConfigFieldInput
              key={field.name}
              pluginId={plugin.id}
              field={field}
              value={values[field.name]}
              onChange={(v) => setField(field.name, v)}
            />
          ))}
        </div>
      )}

      {saveError && <p className="error">{saveError}</p>}
      <p className="row">
        <button onClick={apply} disabled={saving}>
          Apply
        </button>
        {health &&
          (health.healthy ? (
            <span className="pill pill-yes">✓ Healthy</span>
          ) : (
            <span className="pill pill-no">✗ Unhealthy: {health.error}</span>
          ))}
      </p>
    </div>
  );
}

function ConfigFieldInput({
  pluginId,
  field,
  value,
  onChange,
}: {
  pluginId: string;
  field: FormField;
  value: unknown;
  onChange: (value: unknown) => void;
}) {
  const inputId = `config-${pluginId}-${field.name}`;
  switch (field.control) {
    case "checkbox":
      return (
        <label className="row" htmlFor={inputId}>
          <input
            id={inputId}
            type="checkbox"
            checked={Boolean(value)}
            onChange={(e) => onChange(e.target.checked)}
          />
          {field.label}
        </label>
      );
    case "number":
      return (
        <p>
          <label htmlFor={inputId}>{field.label}</label>
          <input
            id={inputId}
            type="number"
            value={value === undefined || value === null ? "" : String(value)}
            onChange={(e) => onChange(e.target.value === "" ? undefined : Number(e.target.value))}
          />
        </p>
      );
    case "secret":
      return (
        <p>
          <label htmlFor={inputId}>{field.label}</label>
          <input
            id={inputId}
            type="password"
            value={String(value ?? "")}
            onChange={(e) => onChange(e.target.value)}
          />
          <small>Leave blank to keep the stored value unchanged.</small>
        </p>
      );
    default:
      return (
        <p>
          <label htmlFor={inputId}>{field.label}</label>
          <input id={inputId} value={String(value ?? "")} onChange={(e) => onChange(e.target.value)} />
        </p>
      );
  }
}

/**
 * PluginEditor adds a new plugin. Config is rendered from the chosen plugin's
 * config_schema once known; for v1 a new plugin has no schema yet, so it offers
 * the raw-JSON config editor as the universal fallback. (Editing an existing
 * plugin uses PluginConfigEditor / the schema-driven form when config_schema
 * is present — see PluginConfigRow above.)
 */
function PluginEditor({ onSave }: { onSave: (input: PluginInput) => void }) {
  const [kind, setKind] = useState<PluginInput["kind"]>("transcriber");
  const [name, setName] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [token, setToken] = useState("");
  const [isDefault, setIsDefault] = useState(false);
  const [rawConfig, setRawConfig] = useState("{}");
  const [jsonError, setJsonError] = useState<string | null>(null);

  function save() {
    let config: Record<string, unknown>;
    try {
      config = JSON.parse(rawConfig) as Record<string, unknown>;
    } catch {
      setJsonError("config must be valid JSON");
      return;
    }
    setJsonError(null);
    onSave({ kind, name, endpoint_url: endpoint, token, config, is_default: isDefault });
  }

  return (
    <div>
      <label htmlFor="new-kind">Kind</label>
      <select id="new-kind" value={kind} onChange={(e) => setKind(e.target.value as PluginInput["kind"])}>
        <option value="transcriber">transcriber</option>
        <option value="agent">agent</option>
      </select>
      <label htmlFor="new-name">Name</label>
      <input id="new-name" value={name} onChange={(e) => setName(e.target.value)} />
      <label htmlFor="new-endpoint">Endpoint URL</label>
      <input id="new-endpoint" value={endpoint} onChange={(e) => setEndpoint(e.target.value)} />
      <label htmlFor="new-token">Plugin auth token</label>
      <input
        id="new-token"
        type="password"
        value={token}
        onChange={(e) => setToken(e.target.value)}
      />
      <small>Shared secret the server presents to this plugin (write-only).</small>
      <label htmlFor="new-config">Config (JSON)</label>
      <textarea id="new-config" rows={4} value={rawConfig} onChange={(e) => setRawConfig(e.target.value)} />
      {jsonError && <p className="error">{jsonError}</p>}
      <label className="row">
        <input type="checkbox" checked={isDefault} onChange={(e) => setIsDefault(e.target.checked)} />
        Set as default for its kind
      </label>
      <p>
        <button onClick={save} disabled={!name || !endpoint || !token}>
          Save plugin
        </button>
      </p>
    </div>
  );
}

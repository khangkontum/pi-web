<script lang="ts">
  // Custom switch — pairs a mono label with an on/off track.
  let {
    checked,
    onChange,
    label,
  }: {
    checked: boolean;
    onChange: (checked: boolean) => void;
    label: string;
  } = $props();
</script>

<button
  type="button"
  role="switch"
  aria-checked={checked}
  class="toggle"
  class:on={checked}
  onclick={() => onChange(!checked)}
>
  <span class="track" aria-hidden="true"><span class="knob"></span></span>
  <span class="name">{label}</span>
</button>

<style>
  .toggle {
    display: inline-flex;
    align-items: center;
    gap: 0.55rem;
    font-size: var(--text-sm);
    color: var(--ink-muted);
  }
  .toggle:hover {
    color: var(--ink);
  }
  .track {
    flex: none;
    width: 28px;
    height: 16px;
    padding: 2px;
    border-radius: 999px;
    background: var(--surface-3);
    border: 1px solid var(--border-strong);
    transition: background 120ms ease;
  }
  .knob {
    display: block;
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--surface);
    border: 1px solid var(--border-strong);
    transition: transform 120ms ease;
  }
  .on .track {
    background: var(--live);
    border-color: var(--live);
  }
  .on .knob {
    transform: translateX(12px);
    border-color: transparent;
  }
  .on .name {
    color: var(--ink);
  }
</style>

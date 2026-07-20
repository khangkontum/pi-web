// A short two-note ping for "turn settled while the tab was in background".
// Synthesized with WebAudio so the binary ships no audio assets.

let ctx: AudioContext | null = null;

export function settlePing(): void {
  try {
    ctx ??= new AudioContext();
    const t0 = ctx.currentTime;
    for (const [freq, at, dur] of [
      [660, 0, 0.09],
      [880, 0.11, 0.16],
    ] as const) {
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.type = "sine";
      osc.frequency.value = freq;
      gain.gain.setValueAtTime(0.0001, t0 + at);
      gain.gain.exponentialRampToValueAtTime(0.06, t0 + at + 0.02);
      gain.gain.exponentialRampToValueAtTime(0.0001, t0 + at + dur);
      osc.connect(gain).connect(ctx.destination);
      osc.start(t0 + at);
      osc.stop(t0 + at + dur + 0.05);
    }
  } catch {
    /* audio unavailable; stay silent */
  }
}

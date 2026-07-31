import { describe, expect, it, vi } from "vitest";

import { TerminalFileSync } from "./terminal-file-sync";

const encoder = new TextEncoder();
const decoder = new TextDecoder();

function bytes(value: string): Uint8Array {
    return encoder.encode(value);
}

function waveFile(size: number, maxSize: number = 1024): WaveFile {
    return {
        zoneid: "zone-1",
        name: "term",
        opts: { circular: true, maxsize: maxSize },
        createdts: 1,
        size,
        modts: 1,
        meta: {},
    };
}

function deferred<T>() {
    let resolve: (value: T) => void;
    let reject: (error: unknown) => void;
    const promise = new Promise<T>((resolvePromise, rejectPromise) => {
        resolve = resolvePromise;
        reject = rejectPromise;
    });
    return { promise, resolve: resolve!, reject: reject! };
}

describe("TerminalFileSync", () => {
    it("fills a missing offset range from WaveFS instead of skipping it", async () => {
        const writes: Array<{ data: string; endOffset: number; contiguous: boolean }> = [];
        const fetchDelta = vi.fn(async (offset: number) => {
            expect(offset).toBe(5);
            return { data: bytes("56789"), fileInfo: waveFile(10) };
        });
        const sync = new TerminalFileSync({
            fetchDelta,
            write: async (data, endOffset, contiguous) => {
                writes.push({ data: decoder.decode(data), endOffset, contiguous });
            },
        });

        sync.reset(5);
        sync.append(bytes("89"), 8);
        await sync.whenIdle();

        expect(fetchDelta).toHaveBeenCalledTimes(1);
        expect(writes).toEqual([{ data: "56789", endOffset: 10, contiguous: true }]);
        expect(sync.offset).toBe(10);
    });

    it("catches up after reconnect even when no later append event arrives", async () => {
        const writes: string[] = [];
        const sync = new TerminalFileSync({
            fetchDelta: async (offset) => {
                expect(offset).toBe(3);
                return { data: bytes("345"), fileInfo: waveFile(6) };
            },
            write: async (data) => {
                writes.push(decoder.decode(data));
            },
        });

        sync.reset(3);
        sync.requestCatchUp("wps-reconnect");
        await sync.whenIdle();

        expect(writes).toEqual(["345"]);
        expect(sync.offset).toBe(6);
    });

    it("keeps one xterm write active and batches appends that arrive behind it", async () => {
        const firstWrite = deferred<void>();
        const secondWrite = deferred<void>();
        const writes: Array<{ data: string; endOffset: number }> = [];
        const sync = new TerminalFileSync({
            fetchDelta: async () => ({ data: new Uint8Array(), fileInfo: waveFile(0) }),
            write: (data, endOffset) => {
                writes.push({ data: decoder.decode(data), endOffset });
                return writes.length === 1 ? firstWrite.promise : secondWrite.promise;
            },
        });

        sync.append(bytes("a"), 0);
        sync.append(bytes("b"), 1);
        sync.append(bytes("c"), 2);
        await Promise.resolve();

        expect(writes).toEqual([{ data: "a", endOffset: 1 }]);

        firstWrite.resolve();
        await vi.waitFor(() => expect(writes).toHaveLength(2));
        expect(writes[1]).toEqual({ data: "bc", endOffset: 3 });

        secondWrite.resolve();
        await sync.whenIdle();
        expect(sync.offset).toBe(3);
    });

    it("splits a large WaveFS catch-up into bounded xterm writes", async () => {
        const maxWriteSize = 64 * 1024;
        const payload = new Uint8Array(maxWriteSize * 2 + 17);
        const writes: Array<{ length: number; endOffset: number }> = [];
        const sync = new TerminalFileSync({
            fetchDelta: async () => ({ data: payload, fileInfo: waveFile(payload.length, payload.length) }),
            write: async (data, endOffset) => {
                writes.push({ length: data.length, endOffset });
            },
        });

        sync.requestCatchUp("large-reconnect-gap");
        await sync.whenIdle();

        expect(writes).toEqual([
            { length: maxWriteSize, endOffset: maxWriteSize },
            { length: maxWriteSize, endOffset: maxWriteSize * 2 },
            { length: 17, endOffset: payload.length },
        ]);
        expect(Math.max(...writes.map((write) => write.length))).toBeLessThanOrEqual(maxWriteSize);
        expect(sync.offset).toBe(payload.length);
    });

    it("stops writing an old chunked batch after the terminal stream is reset", async () => {
        const firstWrite = deferred<void>();
        const payload = new Uint8Array(64 * 1024 * 2);
        const writes: number[] = [];
        const sync = new TerminalFileSync({
            fetchDelta: async () => ({ data: payload, fileInfo: waveFile(payload.length, payload.length) }),
            write: (data) => {
                writes.push(data.length);
                return writes.length === 1 ? firstWrite.promise : Promise.resolve();
            },
        });

        sync.requestCatchUp("before-reset");
        await vi.waitFor(() => expect(writes).toHaveLength(1));
        sync.reset(0);
        firstWrite.resolve();
        await sync.whenIdle();

        expect(writes).toEqual([64 * 1024]);
        expect(sync.offset).toBe(0);
    });

    it("runs another catch-up when a newer gap arrives during an in-flight fetch", async () => {
        const firstFetch = deferred<{ data: Uint8Array; fileInfo: WaveFile }>();
        const fetchOffsets: number[] = [];
        const writes: string[] = [];
        const sync = new TerminalFileSync({
            fetchDelta: async (offset) => {
                fetchOffsets.push(offset);
                if (fetchOffsets.length === 1) {
                    return firstFetch.promise;
                }
                expect(offset).toBe(9);
                return { data: bytes("9"), fileInfo: waveFile(10) };
            },
            write: async (data) => {
                writes.push(decoder.decode(data));
            },
        });

        sync.reset(5);
        sync.append(bytes("8"), 8);
        await Promise.resolve();
        sync.append(bytes("9"), 9);
        firstFetch.resolve({ data: bytes("5678"), fileInfo: waveFile(9) });
        await sync.whenIdle();

        expect(fetchOffsets).toEqual([5, 9]);
        expect(writes.join("")).toBe("56789");
        expect(sync.offset).toBe(10);
    });

    it("does not treat a stale catch-up snapshot as a file replacement after a live append", async () => {
        const firstFetch = deferred<{ data: Uint8Array; fileInfo: WaveFile }>();
        const fetchOffsets: number[] = [];
        const writes: string[] = [];
        const onSourceReset = vi.fn();
        const sync = new TerminalFileSync({
            fetchDelta: async (offset) => {
                fetchOffsets.push(offset);
                if (fetchOffsets.length === 1) {
                    return firstFetch.promise;
                }
                return { data: new Uint8Array(), fileInfo: waveFile(4) };
            },
            write: async (data) => {
                writes.push(decoder.decode(data));
            },
            onSourceReset,
        });

        sync.reset(3);
        sync.requestCatchUp("reconnect");
        sync.append(bytes("3"), 3);
        firstFetch.resolve({ data: new Uint8Array(), fileInfo: waveFile(3) });
        await sync.whenIdle();

        expect(fetchOffsets).toEqual([3]);
        expect(onSourceReset).not.toHaveBeenCalled();
        expect(writes).toEqual(["3"]);
        expect(sync.offset).toBe(4);
    });

    it("ignores an old catch-up response after the terminal stream is reset", async () => {
        const fetchResult = deferred<{ data: Uint8Array; fileInfo: WaveFile }>();
        const write = vi.fn(async () => {});
        const sync = new TerminalFileSync({
            fetchDelta: async () => fetchResult.promise,
            write,
        });

        sync.reset(5);
        sync.append(bytes("8"), 8);
        await Promise.resolve();
        sync.reset(0);
        fetchResult.resolve({ data: bytes("5678"), fileInfo: waveFile(9) });
        await sync.whenIdle();

        expect(write).not.toHaveBeenCalled();
        expect(sync.offset).toBe(0);
    });

    it("keeps its offset after a failed catch-up and succeeds on the next request", async () => {
        const onError = vi.fn();
        const writes: string[] = [];
        let attempt = 0;
        const sync = new TerminalFileSync({
            fetchDelta: async () => {
                attempt++;
                if (attempt === 1) {
                    throw new Error("temporary fetch failure");
                }
                return { data: bytes("345"), fileInfo: waveFile(6) };
            },
            write: async (data) => {
                writes.push(decoder.decode(data));
            },
            onError,
        });

        sync.reset(3);
        sync.requestCatchUp("first-attempt");
        await sync.whenIdle();
        expect(sync.offset).toBe(3);
        expect(writes).toEqual([]);
        expect(onError).toHaveBeenCalledWith(
            "terminal catch-up failed from offset 3 (first-attempt)",
            expect.any(Error)
        );

        sync.requestCatchUp("retry");
        await sync.whenIdle();
        expect(sync.offset).toBe(6);
        expect(writes).toEqual(["345"]);
    });

    it("rewinds to the failed xterm write so WaveFS catch-up can retry it", async () => {
        const fetchOffsets: number[] = [];
        const writes: string[] = [];
        let writeAttempt = 0;
        const sync = new TerminalFileSync({
            fetchDelta: async (offset) => {
                fetchOffsets.push(offset);
                return { data: bytes("345"), fileInfo: waveFile(6) };
            },
            write: async (data) => {
                writeAttempt++;
                if (writeAttempt === 1) {
                    throw new Error("temporary xterm failure");
                }
                writes.push(decoder.decode(data));
            },
        });

        sync.reset(3);
        sync.append(bytes("345"), 3);
        await sync.whenIdle();
        expect(sync.offset).toBe(3);

        sync.requestCatchUp("retry-write");
        await sync.whenIdle();
        expect(fetchOffsets).toEqual([3]);
        expect(writes).toEqual(["345"]);
        expect(sync.offset).toBe(6);
    });

    it("resets the display and refetches from zero when the WaveFS file was replaced", async () => {
        const fetchOffsets: number[] = [];
        const writes: string[] = [];
        const onSourceReset = vi.fn();
        const sync = new TerminalFileSync({
            fetchDelta: async (offset) => {
                fetchOffsets.push(offset);
                if (fetchOffsets.length === 1) {
                    return { data: new Uint8Array(), fileInfo: waveFile(2) };
                }
                return { data: bytes("ab"), fileInfo: waveFile(2) };
            },
            write: async (data) => {
                writes.push(decoder.decode(data));
            },
            onSourceReset,
        });

        sync.reset(8);
        sync.requestCatchUp("missed-truncate");
        await sync.whenIdle();

        expect(fetchOffsets).toEqual([8, 0]);
        expect(onSourceReset).toHaveBeenCalledTimes(1);
        expect(writes).toEqual(["ab"]);
        expect(sync.offset).toBe(2);
    });

    it("waits for an old xterm write before clearing a replaced WaveFS file", async () => {
        const oldWrite = deferred<void>();
        const events: string[] = [];
        let fetchCount = 0;
        const sync = new TerminalFileSync({
            fetchDelta: async () => {
                fetchCount++;
                if (fetchCount === 1) {
                    return { data: new Uint8Array(), fileInfo: waveFile(2) };
                }
                return { data: bytes("ab"), fileInfo: waveFile(2) };
            },
            write: (data) => {
                events.push(`write:${decoder.decode(data)}`);
                return events.length === 1 ? oldWrite.promise : Promise.resolve();
            },
            onSourceReset: () => events.push("reset"),
        });

        sync.reset(8);
        sync.append(bytes("x"), 8);
        sync.requestCatchUp("missed-truncate-during-write");
        await vi.waitFor(() => expect(fetchCount).toBe(2));
        expect(events).toEqual(["write:x"]);

        oldWrite.resolve();
        await sync.whenIdle();

        expect(events).toEqual(["write:x", "reset", "write:ab"]);
        expect(sync.offset).toBe(2);
    });

    it("applies an explicit file replacement after an active xterm write", async () => {
        const oldWrite = deferred<void>();
        const events: string[] = [];
        const sync = new TerminalFileSync({
            fetchDelta: async () => ({ data: new Uint8Array(), fileInfo: waveFile(0) }),
            write: async (data) => {
                events.push(`write:${decoder.decode(data)}`);
                await oldWrite.promise;
            },
            onSourceReset: () => events.push("reset"),
        });

        sync.append(bytes("old"), 0);
        await vi.waitFor(() => expect(events).toEqual(["write:old"]));
        sync.replace(0);
        expect(events).toEqual(["write:old"]);

        oldWrite.resolve();
        await sync.whenIdle();

        expect(events).toEqual(["write:old", "reset"]);
        expect(sync.offset).toBe(0);
    });
});

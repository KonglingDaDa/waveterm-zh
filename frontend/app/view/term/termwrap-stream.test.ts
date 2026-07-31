import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { TermWrap } from "./termwrap";

const encoder = new TextEncoder();

function makeWrap(loaded: boolean = true) {
    const terminalFileSync = {
        append: vi.fn(),
        dispose: vi.fn(),
        replace: vi.fn(),
        requestCatchUp: vi.fn(),
        reset: vi.fn(),
    };
    const wrap = Object.create(TermWrap.prototype) as TermWrap & {
        terminalFileSync: typeof terminalFileSync;
        resyncAfterLoad: boolean;
    };
    Object.assign(wrap, {
        blockId: "block-1",
        doTerminalWrite: vi.fn(),
        heldData: [],
        loaded,
        ptyOffset: 7,
        resyncAfterLoad: false,
        terminal: { clear: vi.fn() },
        terminalFileSync,
    });
    return { terminalFileSync, wrap };
}

describe("TermWrap durable terminal stream integration", () => {
    beforeEach(() => {
        vi.restoreAllMocks();
    });

    afterEach(() => {
        vi.useRealTimers();
        vi.unstubAllGlobals();
    });

    it("routes live blockfile appends through TerminalFileSync", () => {
        const { terminalFileSync, wrap } = makeWrap();

        wrap.writeStreamData(encoder.encode("abc"), 7);

        expect(terminalFileSync.append).toHaveBeenCalledWith(expect.any(Uint8Array), 7);
        expect(wrap.doTerminalWrite).not.toHaveBeenCalled();
    });

    it("splits initial terminal restoration into bounded sequential xterm writes", async () => {
        const { wrap } = makeWrap(false);
        const maxWriteSize = 64 * 1024;
        const writes: Array<{ length: number; setPtyOffset: number }> = [];
        wrap.doTerminalWrite = vi.fn(async (data: Uint8Array, setPtyOffset: number) => {
            writes.push({ length: data.length, setPtyOffset });
        });

        await wrap.writeInitialTerminalData(new Uint8Array(maxWriteSize * 2 + 7), 123);

        expect(writes).toEqual([
            { length: maxWriteSize, setPtyOffset: 123 },
            { length: maxWriteSize, setPtyOffset: 123 },
            { length: 7, setPtyOffset: 123 },
        ]);
    });

    it("replaces file synchronization when the blockfile is truncated", () => {
        const { terminalFileSync, wrap } = makeWrap();

        wrap.handleNewFileSubjectData({
            zoneid: "block-1",
            filename: "term",
            fileop: "truncate",
            data64: "",
            offset: 0,
        });

        expect(terminalFileSync.replace).toHaveBeenCalledWith(0);
        expect(terminalFileSync.reset).not.toHaveBeenCalled();
    });

    it("defers a truncate during initial loading until stale xterm writes have finished", () => {
        const { terminalFileSync, wrap } = makeWrap(false);
        Object.assign(wrap, {
            sourceReplacedAfterLoad: false,
            finishInitialTerminalLoad: TermWrap.prototype.finishInitialTerminalLoad,
        });

        wrap.handleNewFileSubjectData({
            zoneid: "block-1",
            filename: "term",
            fileop: "truncate",
            data64: "",
            offset: 0,
        });

        expect(terminalFileSync.replace).not.toHaveBeenCalled();
        expect(wrap.sourceReplacedAfterLoad).toBe(true);

        wrap.finishInitialTerminalLoad();

        expect(terminalFileSync.reset).toHaveBeenCalledWith(7);
        expect(terminalFileSync.replace).toHaveBeenCalledWith(0);
        expect(terminalFileSync.requestCatchUp).toHaveBeenCalledWith("file-replaced-after-load");
        expect(wrap.loaded).toBe(true);
    });

    it("clears the terminal and offset when the synchronized source is replaced", () => {
        const { wrap } = makeWrap();

        wrap.handleTerminalSourceReset();

        expect(wrap.terminal.clear).toHaveBeenCalledTimes(1);
        expect(wrap.ptyOffset).toBe(0);
    });

    it("catches up immediately after WPS reconnect once initial loading is complete", () => {
        const { terminalFileSync, wrap } = makeWrap();

        wrap.handleWpsReconnect();

        expect(terminalFileSync.requestCatchUp).toHaveBeenCalledWith("wps-reconnect");
    });

    it("defers WPS reconnect catch-up until initial loading completes", () => {
        const { terminalFileSync, wrap } = makeWrap(false);

        wrap.handleWpsReconnect();

        expect(terminalFileSync.requestCatchUp).not.toHaveBeenCalled();
        expect(wrap.resyncAfterLoad).toBe(true);
    });

    it("requests durable catch-up when the user types after visible output went stale", () => {
        const { wrap } = makeWrap();
        wrap.lastUpdated = 0;
        wrap.requestDurableCatchUp = vi.fn();

        wrap.handleTermData("\u0003");

        expect(wrap.requestDurableCatchUp).toHaveBeenCalledWith("terminal-input");
    });

    it("gives the durable catch-up watchdog a deadline when the renderer stays busy", async () => {
        vi.useFakeTimers();
        vi.setSystemTime(20_000);
        const requestIdleCallback = vi.fn((callback: IdleRequestCallback) => {
            callback({ didTimeout: false, timeRemaining: () => 50 });
            return 1;
        });
        vi.stubGlobal("window", { requestIdleCallback });
        const { wrap } = makeWrap();
        Object.assign(wrap, {
            disposed: false,
            idleTimeoutId: null,
            lastUpdated: 0,
            processAndCacheData: vi.fn(),
            requestDurableCatchUp: vi.fn(),
        });

        wrap.runProcessIdleTimeout();
        await vi.advanceTimersByTimeAsync(5000);

        expect(requestIdleCallback).toHaveBeenCalledWith(expect.any(Function), { timeout: 1000 });
        expect(wrap.requestDurableCatchUp).toHaveBeenCalledWith("durable-idle-poll", 15000);
        wrap.disposed = true;
    });
});

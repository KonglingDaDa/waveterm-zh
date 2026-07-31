type TerminalFileDelta = {
    data: Uint8Array;
    fileInfo: WaveFile;
};

type TerminalFileSyncOptions = {
    fetchDelta: (offset: number) => Promise<TerminalFileDelta>;
    write: (data: Uint8Array, endOffset: number, contiguous: boolean) => Promise<void> | void;
    onError?: (message: string, error?: unknown) => void;
    onSourceReset?: () => void;
};

export const MaxTerminalWriteBytes = 64 * 1024;

function joinChunks(chunks: Uint8Array[], totalLength: number): Uint8Array {
    if (chunks.length === 1) {
        return chunks[0];
    }
    const joined = new Uint8Array(totalLength);
    let offset = 0;
    for (const chunk of chunks) {
        joined.set(chunk, offset);
        offset += chunk.length;
    }
    return joined;
}

export class TerminalFileSync {
    private readonly fetchDelta: TerminalFileSyncOptions["fetchDelta"];
    private readonly write: TerminalFileSyncOptions["write"];
    private readonly onError: NonNullable<TerminalFileSyncOptions["onError"]>;
    private readonly onSourceReset: NonNullable<TerminalFileSyncOptions["onSourceReset"]>;

    private acceptedOffset = 0;
    private epoch = 0;
    private disposed = false;

    private pendingChunks: Uint8Array[] = [];
    private pendingLength = 0;
    private pendingEndOffset = 0;
    private pendingContiguous = true;
    private writerRunning = false;

    private catchUpRequested = false;
    private catchUpRunning = false;
    private catchUpReason = "unspecified";
    // A file replacement must clear the display after any old xterm write finishes.
    private sourceResetPending = false;
    private idleWaiters = new Set<() => void>();

    constructor(options: TerminalFileSyncOptions) {
        this.fetchDelta = options.fetchDelta;
        this.write = options.write;
        this.onError = options.onError ?? (() => {});
        this.onSourceReset = options.onSourceReset ?? (() => {});
    }

    get offset(): number {
        return this.acceptedOffset;
    }

    reset(offset: number): void {
        this.epoch++;
        this.acceptedOffset = offset;
        this.pendingChunks = [];
        this.pendingLength = 0;
        this.pendingEndOffset = offset;
        this.pendingContiguous = true;
        this.catchUpRequested = false;
        this.sourceResetPending = false;
        this.notifyIdle();
    }

    replace(offset: number): void {
        if (this.disposed) {
            return;
        }
        this.reset(offset);
        this.sourceResetPending = true;
        if (!this.writerRunning) {
            this.applySourceReset();
        }
    }

    dispose(): void {
        this.disposed = true;
        this.epoch++;
        this.pendingChunks = [];
        this.pendingLength = 0;
        this.catchUpRequested = false;
        this.sourceResetPending = false;
        this.notifyIdle();
    }

    append(data: Uint8Array, offset?: number): void {
        if (this.disposed || data.length === 0) {
            return;
        }
        if (offset == null) {
            this.enqueue(data, this.acceptedOffset + data.length, true);
            return;
        }

        const endOffset = offset + data.length;
        if (endOffset <= this.acceptedOffset) {
            return;
        }
        if (offset > this.acceptedOffset) {
            this.requestCatchUp(`offset-gap:${this.acceptedOffset}-${offset}`);
            return;
        }

        const unseenData = data.subarray(this.acceptedOffset - offset);
        this.enqueue(unseenData, endOffset, true);
    }

    requestCatchUp(reason: string): void {
        if (this.disposed) {
            return;
        }
        this.catchUpReason = reason;
        this.catchUpRequested = true;
        if (!this.catchUpRunning) {
            void this.runCatchUp();
        }
    }

    whenIdle(): Promise<void> {
        if (this.isIdle()) {
            return Promise.resolve();
        }
        return new Promise((resolve) => this.idleWaiters.add(resolve));
    }

    private enqueue(data: Uint8Array, endOffset: number, contiguous: boolean): void {
        if (data.length === 0 || this.disposed) {
            return;
        }
        this.pendingChunks.push(data);
        this.pendingLength += data.length;
        this.pendingEndOffset = endOffset;
        this.pendingContiguous = this.pendingChunks.length === 1 ? contiguous : this.pendingContiguous && contiguous;
        this.acceptedOffset = endOffset;
        if (!this.writerRunning) {
            void this.drainWrites();
        }
    }

    private async drainWrites(): Promise<void> {
        if (this.writerRunning || this.disposed) {
            return;
        }
        this.writerRunning = true;
        try {
            while (!this.disposed && this.pendingChunks.length > 0) {
                this.applySourceReset();
                const chunks = this.pendingChunks;
                const totalLength = this.pendingLength;
                const endOffset = this.pendingEndOffset;
                const contiguous = this.pendingContiguous;
                const writeEpoch = this.epoch;
                this.pendingChunks = [];
                this.pendingLength = 0;
                this.pendingContiguous = true;

                const data = joinChunks(chunks, totalLength);
                const startOffset = endOffset - totalLength;
                for (
                    let writeOffset = 0;
                    writeOffset < data.length && !this.disposed && writeEpoch === this.epoch;
                    writeOffset += MaxTerminalWriteBytes
                ) {
                    const writeEnd = Math.min(writeOffset + MaxTerminalWriteBytes, data.length);
                    try {
                        await this.write(
                            data.subarray(writeOffset, writeEnd),
                            startOffset + writeEnd,
                            contiguous || writeOffset > 0
                        );
                    } catch (error) {
                        if (!this.disposed && writeEpoch === this.epoch) {
                            const failedOffset = startOffset + writeOffset;
                            this.epoch++;
                            this.acceptedOffset = failedOffset;
                            this.pendingChunks = [];
                            this.pendingLength = 0;
                            this.pendingEndOffset = failedOffset;
                            this.pendingContiguous = true;
                            this.catchUpRequested = false;
                            this.onError(`terminal write failed at offset ${failedOffset}`, error);
                        }
                        return;
                    }
                }
            }
        } finally {
            this.writerRunning = false;
            this.applySourceReset();
            this.notifyIdle();
        }
    }

    private async runCatchUp(): Promise<void> {
        if (this.catchUpRunning || this.disposed) {
            return;
        }
        this.catchUpRunning = true;
        try {
            while (!this.disposed && this.catchUpRequested) {
                this.catchUpRequested = false;
                const reason = this.catchUpReason;
                const fetchEpoch = this.epoch;
                const requestedOffset = this.acceptedOffset;
                let delta: TerminalFileDelta;
                try {
                    delta = await this.fetchDelta(requestedOffset);
                } catch (error) {
                    if (!this.disposed && fetchEpoch === this.epoch) {
                        this.onError(`terminal catch-up failed from offset ${requestedOffset} (${reason})`, error);
                    }
                    continue;
                }
                if (this.disposed || fetchEpoch !== this.epoch || delta?.fileInfo == null) {
                    continue;
                }

                const data = delta.data ?? new Uint8Array();
                const fileEndOffset = delta.fileInfo.size;
                if (fileEndOffset < this.acceptedOffset) {
                    if (this.acceptedOffset !== requestedOffset) {
                        continue;
                    }
                    this.replace(0);
                    this.catchUpRequested = true;
                    continue;
                }
                if (data.length === 0 || fileEndOffset <= this.acceptedOffset) {
                    continue;
                }

                const dataStartOffset = fileEndOffset - data.length;
                if (dataStartOffset > this.acceptedOffset) {
                    this.onError(
                        `terminal catch-up source was truncated: expected ${this.acceptedOffset}, got ${dataStartOffset} (${reason})`
                    );
                    this.acceptedOffset = dataStartOffset;
                    this.enqueue(data, fileEndOffset, false);
                    continue;
                }

                const unseenData = data.subarray(this.acceptedOffset - dataStartOffset);
                this.enqueue(unseenData, fileEndOffset, true);
            }
        } finally {
            this.catchUpRunning = false;
            if (!this.writerRunning) {
                this.applySourceReset();
            }
            this.notifyIdle();
        }
    }

    private applySourceReset(): void {
        if (!this.sourceResetPending || this.disposed) {
            return;
        }
        this.sourceResetPending = false;
        try {
            this.onSourceReset();
        } catch (error) {
            this.onError("terminal source reset failed", error);
        }
    }

    private isIdle(): boolean {
        return !this.catchUpRunning && !this.writerRunning && this.pendingChunks.length === 0;
    }

    private notifyIdle(): void {
        if (!this.isIdle()) {
            return;
        }
        const waiters = Array.from(this.idleWaiters);
        this.idleWaiters.clear();
        for (const resolve of waiters) {
            resolve();
        }
    }
}

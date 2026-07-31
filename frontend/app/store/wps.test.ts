import { describe, expect, it, vi } from "vitest";

import { getFileSubject, publishFileSubject, subscribeToWpsReconnect, wpsReconnectHandler } from "./wps";

function appendEvent(data64: string = "YQ=="): WSFileEventData {
    return {
        zoneid: "zone-1",
        filename: "term",
        fileop: "append",
        data64,
        offset: 0,
    };
}

describe("WPS file subjects", () => {
    it("publishes without taking a consumer reference", () => {
        const subject = getFileSubject("zone-1", "term");
        const handler = vi.fn();
        const subscription = subject.subscribe(handler);

        try {
            publishFileSubject(appendEvent());

            expect(handler).toHaveBeenCalledTimes(1);
            expect(subject.refCount).toBe(1);
        } finally {
            subscription.unsubscribe();
            subject.release();
        }

        const replacement = getFileSubject("zone-1", "term");
        try {
            expect(replacement).not.toBe(subject);
        } finally {
            replacement.release();
        }
    });
});

describe("WPS reconnect listeners", () => {
    it("notifies active listeners and stops after unsubscribe", () => {
        const handler = vi.fn();
        const unsubscribe = subscribeToWpsReconnect(handler);

        wpsReconnectHandler();
        expect(handler).toHaveBeenCalledTimes(1);

        unsubscribe();
        wpsReconnectHandler();
        expect(handler).toHaveBeenCalledTimes(1);
    });
});

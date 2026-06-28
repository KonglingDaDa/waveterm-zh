// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { afterEach, describe, expect, it } from "vitest";
import { clearMissingTranslationKeys, getMissingTranslationKeys, normalizeLocale, resolveLocale, supportedLocales, t } from "./index";

// en-US catalog is intentionally empty; English call-site strings are the fallback keys.
// zh-CN is the translation coverage catalog — tests here verify zh-CN resolution, not en-US/zh-CN parity.
describe("i18n", () => {
    afterEach(() => {
        clearMissingTranslationKeys();
    });

    it("defaults to English source strings", () => {
        expect(t("Save", undefined, "en-US")).toBe("Save");
        expect(t("Open File...", undefined, "en-US")).toBe("Open File...");
    });

    it("returns Simplified Chinese translations", () => {
        expect(t("Save", undefined, "zh-CN")).toBe("保存");
        expect(t("Open File...", undefined, "zh-CN")).toBe("打开文件...");
    });

    it("translates language menu labels", () => {
        expect(supportedLocales.map((option) => t(option.label, undefined, "zh-CN"))).toEqual(["英文", "简体中文"]);
    });

    it("interpolates translated messages", () => {
        expect(t("Client Version {version}", { version: "0.14.5" }, "zh-CN")).toBe("客户端版本 0.14.5");
        expect(t("Open Clipboard URL ({host})", { host: "example.com" }, "zh-CN")).toBe(
            "打开剪贴板 URL（example.com）"
        );
    });

    it("falls back to the original key when a translation is missing", () => {
        expect(t("Untranslated UI String", undefined, "zh-CN")).toBe("Untranslated UI String");
        expect(getMissingTranslationKeys()).toContain("Untranslated UI String");
    });

    it("handles null and undefined keys safely", () => {
        expect(t(null, undefined, "zh-CN")).toBe("");
        expect(t(undefined, undefined, "zh-CN")).toBe("");
    });

    it("resolves supported locales", () => {
        expect(resolveLocale(null)).toBe("en-US");
        expect(resolveLocale("en")).toBe("en-US");
        expect(resolveLocale("zh-CN")).toBe("zh-CN");
        expect(resolveLocale("zh_Hans_CN")).toBe("zh-CN");
        expect(resolveLocale("fr-FR")).toBe("en-US");
        expect(normalizeLocale("en")).toBe("en-US");
        expect(normalizeLocale("zh_Hans_CN")).toBe("zh-CN");
        expect(normalizeLocale("ja-JP")).toBe(null);
    });
});
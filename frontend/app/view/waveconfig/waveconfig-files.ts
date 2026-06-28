// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { t, type Locale } from "@/app/i18n";
import type * as React from "react";

type ValidationResult = { success: true } | { error: string };
type ConfigValidator = (parsed: any, locale: Locale) => ValidationResult;

export type ConfigFile = {
    name: string;
    path: string;
    language?: string;
    deprecated?: boolean;
    description?: string;
    docsUrl?: string;
    validator?: ConfigValidator;
    isSecrets?: boolean;
    hasJsonView?: boolean;
    visualComponent?: React.ComponentType<{ model: ViewModel }>;
};

function validateAiJson(parsed: any, locale: Locale): ValidationResult {
    const keys = Object.keys(parsed);
    for (const key of keys) {
        if (!key.startsWith("ai@")) {
            return {
                error: t('Invalid key "{key}": all top-level keys must start with "ai@"', { key }, locale),
            };
        }
    }
    return { success: true };
}

function validateWaveAiJson(parsed: any, locale: Locale): ValidationResult {
    const keys = Object.keys(parsed);
    const keyPattern = /^[a-zA-Z0-9_@.-]+$/;
    for (const key of keys) {
        if (!keyPattern.test(key)) {
            return {
                error: t(
                    'Invalid key "{key}": keys must only contain letters, numbers, underscores, @, dots, and hyphens',
                    { key },
                    locale
                ),
            };
        }
    }
    return { success: true };
}

export function makeConfigFiles(isWindows: boolean, locale: Locale, secretsContent: ConfigFile["visualComponent"]): ConfigFile[] {
    const tl = (key: string) => t(key, undefined, locale);
    return [
        {
            name: tl("General"),
            path: "settings.json",
            language: "json",
            docsUrl: "https://docs.waveterm.dev/config",
            hasJsonView: true,
        },
        {
            name: tl("Connections"),
            path: "connections.json",
            language: "json",
            docsUrl: "https://docs.waveterm.dev/connections",
            description: isWindows ? tl("SSH hosts and WSL distros") : tl("SSH hosts"),
            hasJsonView: true,
        },
        {
            name: tl("Sidebar Widgets"),
            path: "widgets.json",
            language: "json",
            docsUrl: "https://docs.waveterm.dev/customwidgets",
            hasJsonView: true,
        },
        {
            name: tl("Wave AI Modes"),
            path: "waveai.json",
            language: "json",
            description: tl("Local models and BYOK"),
            docsUrl: "https://docs.waveterm.dev/waveai-modes",
            validator: validateWaveAiJson,
            hasJsonView: true,
        },
        {
            name: tl("Tab Backgrounds"),
            path: "backgrounds.json",
            language: "json",
            docsUrl: "https://docs.waveterm.dev/tab-backgrounds",
            hasJsonView: true,
        },
        {
            name: tl("Secrets"),
            path: "secrets",
            isSecrets: true,
            hasJsonView: false,
            visualComponent: secretsContent,
        },
    ];
}

export function makeDeprecatedConfigFiles(locale: Locale): ConfigFile[] {
    const tl = (key: string) => t(key, undefined, locale);
    return [
        {
            name: tl("Presets"),
            path: "presets.json",
            language: "json",
            deprecated: true,
            hasJsonView: true,
        },
        {
            name: tl("AI Presets"),
            path: "presets/ai.json",
            language: "json",
            deprecated: true,
            docsUrl: "https://docs.waveterm.dev/ai-presets",
            validator: validateAiJson,
            hasJsonView: true,
        },
    ];
}

export function formatConfigErrorLine(cerr: ConfigError, locale: Locale): string {
    return t("Configuration error in {file}: {err}", { file: cerr.file, err: cerr.err }, locale);
}
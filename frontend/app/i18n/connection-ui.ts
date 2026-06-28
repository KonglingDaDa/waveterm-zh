// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { t, type Locale } from "./index";

export function connectionSectionLocal(locale: Locale): string {
    return t("Local", undefined, locale);
}

export function connectionSectionRemote(locale: Locale): string {
    return t("Remote", undefined, locale);
}

export function reconnectLabel(connection: string, locale: Locale): string {
    return t("Reconnect to {connection}", { connection }, locale);
}

export function disconnectLabel(connection: string, locale: Locale): string {
    return t("Disconnect {connection}", { connection }, locale);
}

export function newConnectionLabel(name: string, locale: Locale): string {
    return t("{name} (New Connection)", { name }, locale);
}

export function editConnectionsLabel(locale: Locale): string {
    return t("Edit Connections", undefined, locale);
}

export function connectPlaceholder(locale: Locale): string {
    return t("Connect to (username@host)...", undefined, locale);
}
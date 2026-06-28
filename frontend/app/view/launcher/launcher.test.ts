// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import type { BlockNodeModel } from "@/app/block/blocktypes";
import { globalStore } from "@/app/store/jotaiStore";
import type { TabModel } from "@/app/store/tab-model";
import { describe, expect, it, vi } from "vitest";
import { LauncherViewModel } from "./launcher";

describe("LauncherViewModel", () => {
    it("Enter key launches the selected widget config", () => {
        const mockWidget = {
            label: "terminal",
            blockdef: { meta: { view: "term", controller: "shell" } },
        } as WidgetConfigType;

        const model = new LauncherViewModel({
            blockId: "block-1",
            nodeModel: {} as BlockNodeModel,
            tabModel: {} as TabModel,
            waveEnv: {} as ViewModelInitType["waveEnv"],
        });
        model.gridLayout = { columns: 3, tileWidth: 90, tileHeight: 90, showLabel: true };

        vi.spyOn(globalStore, "get").mockImplementation((atom) => {
            if (atom === model.filteredWidgetsAtom) {
                return [{ id: "defwidget@terminal", widget: mockWidget }];
            }
            if (atom === model.selectedIndex) {
                return 0;
            }
            return undefined;
        });

        const handleWidgetSelect = vi.spyOn(model, "handleWidgetSelect").mockResolvedValue(undefined);
        const enterEvent = { key: "Enter" } as WaveKeyboardEvent;

        expect(model.keyDownHandler(enterEvent)).toBe(true);
        expect(handleWidgetSelect).toHaveBeenCalledOnce();
        expect(handleWidgetSelect).toHaveBeenCalledWith(mockWidget);
    });
});
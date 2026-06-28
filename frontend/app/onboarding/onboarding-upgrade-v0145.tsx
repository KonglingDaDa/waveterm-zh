// Copyright 2026, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { useT } from "@/app/i18n/react";

const UpgradeOnboardingModal_v0_14_5_Content = () => {
    const tt = useT();
    return (
        <div className="flex flex-col items-start gap-6 w-full mb-4 unselectable">
            <div className="text-secondary leading-relaxed">
                <p className="mb-0">
                    {tt(
                        "Wave v0.14.5 introduces a new Process Viewer widget, several quality-of-life improvements, and a fix for creating new config files from the Settings widget."
                    )}
                </p>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-list-tree"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Process Viewer")}</div>
                    <div className="text-secondary leading-5">
                        {tt(
                            "New widget that displays running processes on local and remote machines, with CPU and memory usage and sortable columns."
                        )}
                    </div>
                </div>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-wrench"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Other Changes")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("Quake Mode")}</strong> &mdash; {tt("global hotkey (")}{" "}
                                <code>app:globalhotkey</code>
                                {tt(") now toggles a Wave window visible and invisible")}
                            </li>
                            <li>
                                <strong>{tt("Drag & Drop Files into Terminal")}</strong>{" "}
                                {tt("to paste their quoted path")}
                            </li>
                            <li>
                                {tt("New")} <code>app:showsplitbuttons</code>{" "}
                                {tt("setting adds split buttons to block headers")}
                            </li>
                            <li>{tt("Toggle the widgets sidebar on and off from the View menu")}</li>
                            <li>{tt("F2 to rename the active tab")}</li>
                            <li>{tt("Mouse back/forward buttons now navigate in web widgets")}</li>
                            <li>
                                <strong>[bugfix]</strong>{" "}
                                {tt(
                                    "Config files that didn't exist yet couldn't be created or edited from the Settings widget"
                                )}
                            </li>
                        </ul>
                    </div>
                </div>
            </div>
        </div>
    );
};

UpgradeOnboardingModal_v0_14_5_Content.displayName = "UpgradeOnboardingModal_v0_14_5_Content";

export { UpgradeOnboardingModal_v0_14_5_Content };
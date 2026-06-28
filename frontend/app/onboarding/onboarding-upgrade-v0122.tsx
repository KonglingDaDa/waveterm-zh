// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

import { useT } from "@/app/i18n/react";

const UpgradeOnboardingModal_v0_12_2_Content = () => {
    const tt = useT();
    return (
        <div className="flex flex-col items-start gap-6 w-full mb-4 unselectable">
            <div className="text-secondary leading-relaxed">
                <p className="mb-0">
                    {tt(
                        "Wave AI can now create and modify files with visual diff previews and easy rollback capabilities. Plus performance improvements and bug fixes."
                    )}
                </p>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-file-pen"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">{tt("Wave AI File Editing")}</div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>
                                <strong>{tt("File Write Tool")}</strong> -{" "}
                                {tt("Wave AI can now create and modify files with your approval")}
                            </li>
                            <li>
                                <strong>{tt("Visual Diff Preview")}</strong> -{" "}
                                {tt("See exactly what will change before approving edits")}
                            </li>
                            <li>
                                <strong>{tt("Easy Rollback")}</strong> -{" "}
                                {tt('Revert file changes with a simple "Revert File" button')}
                            </li>
                        </ul>
                    </div>
                </div>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-sparkles"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">
                        {tt("Additional AI Improvements")}
                    </div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>{tt("Drag & drop files from preview viewer directly to Wave AI")}</li>
                            <li>
                                {tt("Directory listings support in")}{" "}
                                <span className="font-mono">`wsh ai`</span> {tt("commands")}
                            </li>
                            <li>{tt("Adjustable thinking level and max output tokens per chat")}</li>
                            <li>{tt("Improved tool descriptions and input validations")}</li>
                        </ul>
                    </div>
                </div>
            </div>

            <div className="flex w-full items-start gap-4">
                <div className="flex-shrink-0">
                    <i className="text-[24px] text-accent fa-solid fa-wrench"></i>
                </div>
                <div className="flex flex-col items-start gap-2 flex-1">
                    <div className="text-foreground text-base font-semibold leading-[18px]">
                        {tt("Bug Fixes & Improvements")}
                    </div>
                    <div className="text-secondary leading-5">
                        <ul className="list-disc list-outside space-y-1 pl-5">
                            <li>{tt("Fixed significant memory leak in the RPC system")}</li>
                            <li>{tt("Config file schema validation restored")}</li>
                            <li>{tt("Fixed PowerShell 5.x regression")}</li>
                        </ul>
                    </div>
                </div>
            </div>
        </div>
    );
};

UpgradeOnboardingModal_v0_12_2_Content.displayName = "UpgradeOnboardingModal_v0_12_2_Content";

export { UpgradeOnboardingModal_v0_12_2_Content };
export function supportBundlePresentation(bundle) {
  const status = bundle?.status || (bundle?.checks?.fail > 0 ? 'action-required' : bundle?.checks?.warn > 0 ? 'attention' : 'healthy');
  if (status === 'action-required') {
    return {tone: 'failed', label: 'Action required', summary: 'At least one check failed. Review the recovery steps before continuing affected work.'};
  }
  if (status === 'attention') {
    return {tone: 'partial', label: 'Needs attention', summary: 'Core operation remains available, but one or more conditions should be reviewed.'};
  }
  return {tone: 'passed', label: 'No current faults', summary: 'The current bounded checks found no condition that requires action.'};
}

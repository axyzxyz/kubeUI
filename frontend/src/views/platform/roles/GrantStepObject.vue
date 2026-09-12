<script setup lang="ts">
import { computed } from 'vue';
import type { GrantObjectType, RoleGroupItem, RoleItem } from '@/api/types';

const props = defineProps<{
  objectType: GrantObjectType;
  objectId: number | undefined;
  roles: RoleItem[];
  roleGroups: RoleGroupItem[];
}>();

const emit = defineEmits<{
  'update:objectType': [value: GrantObjectType];
  'update:objectId': [value: number | undefined];
}>();

const objectLabel = computed(() => (props.objectType === 'role' ? '角色' : '角色组'));
</script>

<template>
  <div class="step-block">
    <div class="step-title">② 对象(授予什么)</div>
    <el-form label-width="90px">
      <el-form-item label="对象类型">
        <el-radio-group
          :model-value="props.objectType"
          @update:model-value="(v: GrantObjectType) => emit('update:objectType', v)"
        >
          <el-radio-button value="role">角色</el-radio-button>
          <el-radio-button value="role_group">角色组</el-radio-button>
        </el-radio-group>
      </el-form-item>
      <el-form-item :label="objectLabel">
        <el-select
          :model-value="props.objectId"
          :placeholder="`选择${objectLabel}`"
          filterable
          style="width: 100%"
          @update:model-value="(v: number) => emit('update:objectId', v)"
        >
          <template v-if="props.objectType === 'role'">
            <el-option v-for="r in props.roles" :key="r.id" :label="r.name" :value="r.id" />
          </template>
          <template v-else>
            <el-option v-for="g in props.roleGroups" :key="g.id" :label="g.name" :value="g.id" />
          </template>
        </el-select>
      </el-form-item>
    </el-form>
  </div>
</template>

<style scoped>
.step-block {
  margin-bottom: 16px;
}
.step-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-1);
  margin-bottom: 10px;
}
</style>
